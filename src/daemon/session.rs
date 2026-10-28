use std::collections::BTreeMap;
use std::io::Read;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::mpsc::{self, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use anyhow::Result;
use portable_pty::{CommandBuilder, MasterPty, PtySize, native_pty_system};

use crate::config::SessionConfig;

/// Taille d'historique rejouée à un nouveau client ; le tampon peut monter à 2× avant d'être rogné.
const RING_CAP: usize = 2 * 1024 * 1024;
const DEFAULT_SIZE: (u16, u16) = (24, 80);
const STOP_TIMEOUT: Duration = Duration::from_secs(10);

static NEXT_RUN_ID: AtomicU64 = AtomicU64::new(1);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Status {
    Running,
    Stopping,
    Exited(i32),
}

/// Sortie brute d'une exécution : historique rejouable et abonnés au flux en direct.
struct Output {
    ring: Vec<u8>,
    subscribers: Vec<Sender<Vec<u8>>>,
    /// Le PTY est fermé : plus aucune sortie à venir.
    closed: bool,
}

impl Output {
    fn push(&mut self, chunk: &[u8]) {
        self.ring.extend_from_slice(chunk);
        if self.ring.len() > 2 * RING_CAP {
            // On rogne sur une fin de ligne pour ne pas rejouer une ligne coupée en deux.
            let cut = self.ring.len() - RING_CAP;
            let start = self.ring[cut..].iter().position(|&b| b == b'\n').map_or(cut, |p| cut + p + 1);
            self.ring.drain(..start);
        }
        self.subscribers.retain(|tx| tx.send(chunk.to_vec()).is_ok());
    }
}

/// Abonnement à la sortie d'une exécution : l'historique, puis le direct.
pub struct Subscription {
    pub run_id: u64,
    pub replay: Vec<u8>,
    /// Se ferme quand l'exécution est terminée.
    pub live: Receiver<Vec<u8>>,
}

/// Une exécution du processus : un nouveau `Run` est créé à chaque démarrage,
/// pour que les threads d'un ancien run ne touchent jamais l'état du suivant.
pub struct Run {
    pub id: u64,
    output: Mutex<Output>,
    pub status: Mutex<Status>,
    pub started_at: Instant,
    pid: u32,
    master: Mutex<Box<dyn MasterPty + Send>>,
}

impl Run {
    /// Prend l'historique et s'abonne dans la même section critique : aucun octet
    /// n'est perdu ni rejoué deux fois entre les deux.
    fn subscribe(&self) -> Subscription {
        let mut output = self.output.lock().unwrap();
        let (tx, live) = mpsc::channel();
        if !output.closed {
            output.subscribers.push(tx);
        }
        Subscription { run_id: self.id, replay: output.ring.clone(), live }
    }
}

/// Ce qui définit le lancement d'une session : un changement ici ne s'applique
/// qu'au prochain démarrage.
#[derive(Clone, PartialEq, Eq)]
struct Spec {
    cwd: PathBuf,
    command: String,
    shell: String,
    env: BTreeMap<String, String>,
}

impl Spec {
    fn new(config: &SessionConfig, base: &std::path::Path, shell: &str) -> Self {
        Spec {
            cwd: match &config.cwd {
                Some(dir) => base.join(dir),
                None => base.to_path_buf(),
            },
            command: config.command.clone(),
            shell: shell.to_string(),
            env: config.env.iter().map(|(k, v)| (k.clone(), v.clone())).collect(),
        }
    }
}

pub struct Session {
    pub name: String,
    pub group: String,
    spec: Spec,
    /// La config a changé alors que la session tournait : elle utilise encore l'ancienne.
    pub outdated: bool,
    pub run: Option<Arc<Run>>,
    /// Dernière taille de panneau connue, appliquée aux (re)démarrages.
    size: (u16, u16),
    restart_pending: Arc<AtomicBool>,
}

impl Session {
    pub fn new(config: &SessionConfig, base: &std::path::Path, shell: &str) -> Self {
        Session {
            name: config.name.clone(),
            group: group_of(config),
            spec: Spec::new(config, base, shell),
            outdated: false,
            run: None,
            size: DEFAULT_SIZE,
            restart_pending: Arc::new(AtomicBool::new(false)),
        }
    }

    /// Applique une nouvelle définition. Renvoie `true` si le lancement a changé ; une
    /// session en cours n'est pas touchée, elle est seulement marquée `outdated`.
    pub fn update(&mut self, config: &SessionConfig, base: &std::path::Path, shell: &str) -> bool {
        self.group = group_of(config);
        let spec = Spec::new(config, base, shell);
        if spec == self.spec {
            return false;
        }
        self.spec = spec;
        self.outdated = self.is_active();
        true
    }

    pub fn status(&self) -> Option<Status> {
        self.run.as_ref().map(|run| *run.status.lock().unwrap())
    }

    pub fn is_active(&self) -> bool {
        matches!(self.status(), Some(Status::Running | Status::Stopping))
    }

    pub fn start(&mut self) -> Result<()> {
        if self.is_active() {
            return Ok(());
        }
        self.restart_pending.store(false, Ordering::SeqCst);
        self.outdated = false;

        let (rows, cols) = self.size;
        let pair = native_pty_system().openpty(PtySize { rows, cols, pixel_width: 0, pixel_height: 0 })?;
        let mut cmd = CommandBuilder::new(&self.spec.shell);
        cmd.args(["-c", &self.spec.command]);
        cmd.cwd(&self.spec.cwd);
        cmd.env("TERM", "xterm-256color");
        for (key, value) in &self.spec.env {
            cmd.env(key, value);
        }

        let mut child = pair.slave.spawn_command(cmd)?;
        drop(pair.slave);
        let mut reader = pair.master.try_clone_reader()?;

        let run = Arc::new(Run {
            id: NEXT_RUN_ID.fetch_add(1, Ordering::Relaxed),
            output: Mutex::new(Output { ring: Vec::new(), subscribers: Vec::new(), closed: false }),
            status: Mutex::new(Status::Running),
            started_at: Instant::now(),
            pid: child.process_id().unwrap_or(0),
            master: Mutex::new(pair.master),
        });

        let reading = Arc::clone(&run);
        thread::spawn(move || {
            let mut buf = [0u8; 8192];
            while let Ok(n) = reader.read(&mut buf) {
                if n == 0 {
                    break;
                }
                reading.output.lock().unwrap().push(&buf[..n]);
            }
            let mut output = reading.output.lock().unwrap();
            output.closed = true;
            output.subscribers.clear();
        });

        let waiting = Arc::clone(&run);
        thread::spawn(move || {
            let code = child.wait().map(|s| s.exit_code() as i32).unwrap_or(-1);
            *waiting.status.lock().unwrap() = Status::Exited(code);
        });

        self.run = Some(run);
        Ok(())
    }

    /// SIGTERM sur tout le groupe de processus, puis SIGKILL après le délai.
    pub fn stop(&mut self) {
        let Some(run) = &self.run else { return };
        {
            let mut status = run.status.lock().unwrap();
            if *status != Status::Running {
                return;
            }
            *status = Status::Stopping;
        }
        signal_group(run.pid, libc::SIGTERM);

        let run = Arc::clone(run);
        thread::spawn(move || {
            let deadline = Instant::now() + STOP_TIMEOUT;
            while Instant::now() < deadline {
                if matches!(*run.status.lock().unwrap(), Status::Exited(_)) {
                    return;
                }
                thread::sleep(Duration::from_millis(100));
            }
            signal_group(run.pid, libc::SIGKILL);
        });
    }

    /// Arrête puis relance dès que le processus est terminé (voir `tick`).
    pub fn restart(&mut self) -> Result<()> {
        if self.is_active() {
            self.restart_pending.store(true, Ordering::SeqCst);
            self.stop();
            Ok(())
        } else {
            self.start()
        }
    }

    /// À appeler régulièrement : finalise un redémarrage en attente.
    pub fn tick(&mut self) {
        if self.restart_pending.load(Ordering::SeqCst) && !self.is_active() {
            let _ = self.start();
        }
    }

    /// Ajuste la taille du PTY ; mémorisée pour les prochains démarrages.
    pub fn resize(&mut self, rows: u16, cols: u16) {
        if self.size == (rows, cols) {
            return;
        }
        self.size = (rows, cols);
        if let Some(run) = &self.run {
            let _ = run.master.lock().unwrap().resize(PtySize { rows, cols, pixel_width: 0, pixel_height: 0 });
        }
    }

    pub fn subscribe(&self) -> Option<Subscription> {
        self.run.as_ref().map(|run| run.subscribe())
    }
}

fn group_of(config: &SessionConfig) -> String {
    config.group.clone().unwrap_or_else(|| "autres".to_string())
}

fn signal_group(pid: u32, signal: i32) {
    if pid == 0 {
        return;
    }
    // Le PTY fait du fils le leader de sa session : pgid == pid.
    unsafe {
        libc::kill(-(pid as i32), signal);
    }
}

/// Arrête toutes les sessions actives et attend leur fin (SIGKILL après le délai).
pub fn shutdown(sessions: &mut [Session]) {
    for session in sessions.iter_mut() {
        session.restart_pending.store(false, Ordering::SeqCst);
        session.stop();
    }
    let deadline = Instant::now() + STOP_TIMEOUT + Duration::from_secs(1);
    while Instant::now() < deadline && sessions.iter().any(Session::is_active) {
        thread::sleep(Duration::from_millis(50));
    }
}
