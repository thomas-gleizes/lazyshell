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

use super::probe::{self, Probe, Readiness};
use super::schedule::Wait;
use super::usage::{Sampler, Usage};
use crate::config::{Kind, ReadyConfig, SessionConfig};

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
            let start = self.ring[cut..]
                .iter()
                .position(|&b| b == b'\n')
                .map_or(cut, |p| cut + p + 1);
            self.ring.drain(..start);
        }
        self.subscribers
            .retain(|tx| tx.send(chunk.to_vec()).is_ok());
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
    /// L'arrêt de ce processus a été demandé : sa sortie ne vaut pas réussite.
    stop_requested: AtomicBool,
    sampler: Mutex<Sampler>,
    /// Définition avec laquelle ce processus a été lancé.
    spec: Spec,
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
        Subscription {
            run_id: self.id,
            replay: output.ring.clone(),
            live,
        }
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
    ready: Option<ReadyConfig>,
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
            env: config
                .env
                .iter()
                .map(|(k, v)| (k.clone(), v.clone()))
                .collect(),
            ready: config.ready.clone(),
        }
    }
}

pub struct Session {
    pub name: String,
    pub group: String,
    pub kind: Kind,
    pub depends_on: Vec<String>,
    /// Démarrage demandé mais retenu par les dépendances ; `None` sinon.
    pub wait: Option<Wait>,
    /// Sonde `ready` de l'exécution en cours.
    probe: Option<Probe>,
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
            kind: config.kind,
            depends_on: config.depends_on.clone(),
            wait: None,
            probe: None,
            spec: Spec::new(config, base, shell),
            outdated: false,
            run: None,
            size: DEFAULT_SIZE,
            restart_pending: Arc::new(AtomicBool::new(false)),
        }
    }

    /// Applique une nouvelle définition. Renvoie `true` si le lancement ou les dépendances
    /// ont changé ; une session en cours n'est pas touchée, et n'est marquée `outdated`
    /// que si son lancement diffère.
    pub fn update(&mut self, config: &SessionConfig, base: &std::path::Path, shell: &str) -> bool {
        self.group = group_of(config);
        let rewired = self.kind != config.kind || self.depends_on != config.depends_on;
        self.kind = config.kind;
        self.depends_on = config.depends_on.clone();
        let spec = Spec::new(config, base, shell);
        if spec == self.spec {
            return rewired;
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

    /// La tâche a réussi avec la définition actuelle : inutile de la rejouer.
    pub fn has_succeeded(&self) -> bool {
        !self.was_stopped()
            && self.run.as_ref().is_some_and(|run| {
                *run.status.lock().unwrap() == Status::Exited(0) && run.spec == self.spec
            })
    }

    /// L'arrêt de l'exécution en cours (ou dernière) a été demandé.
    pub fn was_stopped(&self) -> bool {
        self.run
            .as_ref()
            .is_some_and(|run| run.stop_requested.load(Ordering::SeqCst))
    }

    pub fn restart_pending(&self) -> bool {
        self.restart_pending.load(Ordering::SeqCst)
    }

    /// Disponibilité de l'exécution en cours ; sans sonde, un processus lancé est prêt.
    pub fn readiness(&self) -> Readiness {
        self.probe
            .as_ref()
            .map_or(Readiness::Ready, Probe::readiness)
    }

    /// Redonne sa chance à une sonde expirée, sans relancer le service.
    pub fn rearm_probe(&mut self) {
        if let Some(probe) = &mut self.probe {
            probe.rearm();
        }
    }

    /// Lance le processus tout de suite, sans regarder les dépendances (voir `Manager`).
    pub fn start(&mut self) -> Result<()> {
        if self.is_active() {
            return Ok(());
        }
        self.restart_pending.store(false, Ordering::SeqCst);
        self.outdated = false;
        self.probe = None;

        let (rows, cols) = self.size;
        let pair = native_pty_system().openpty(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        })?;
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

        let started_at = Instant::now();
        let pid = child.process_id().unwrap_or(0);
        let run = Arc::new(Run {
            id: NEXT_RUN_ID.fetch_add(1, Ordering::Relaxed),
            output: Mutex::new(Output {
                ring: Vec::new(),
                subscribers: Vec::new(),
                closed: false,
            }),
            status: Mutex::new(Status::Running),
            started_at,
            pid,
            stop_requested: AtomicBool::new(false),
            sampler: Mutex::new(Sampler::new(pid, started_at)),
            spec: self.spec.clone(),
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
        self.wait = None;
        self.probe = self.spec.ready.clone().map(|ready| {
            Probe::new(
                ready,
                probe::Context {
                    shell: self.spec.shell.clone(),
                    cwd: self.spec.cwd.clone(),
                    env: self.spec.env.clone(),
                },
            )
        });
        Ok(())
    }

    /// Arrêt demandé par l'utilisateur : annule aussi l'attente et le redémarrage prévus.
    pub fn cancel_and_stop(&mut self) {
        self.wait = None;
        self.restart_pending.store(false, Ordering::SeqCst);
        self.stop();
    }

    /// SIGTERM sur tout le groupe de processus, puis SIGKILL après le délai.
    fn stop(&mut self) {
        if let Some(probe) = &mut self.probe {
            probe.cancel();
        }
        let Some(run) = &self.run else { return };
        {
            let mut status = run.status.lock().unwrap();
            if *status != Status::Running {
                return;
            }
            *status = Status::Stopping;
            run.stop_requested.store(true, Ordering::SeqCst);
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

    /// Arrête le processus en cours ; il sera relancé dès sa fin (voir `take_restart`).
    pub fn request_restart(&mut self) {
        self.restart_pending.store(true, Ordering::SeqCst);
        self.stop();
    }

    /// `true`, une seule fois, quand un redémarrage prévu peut avoir lieu.
    pub fn take_restart(&mut self) -> bool {
        !self.is_active() && self.restart_pending.swap(false, Ordering::SeqCst)
    }

    /// À appeler régulièrement : mesure la consommation et fait avancer la sonde.
    pub fn tick(&mut self) {
        // Mesure le CPU à intervalle régulier : le pourcentage est un écart entre deux relevés.
        if self.is_active() {
            let _ = self.usage();
        }
        let running = self.status() == Some(Status::Running);
        if let Some(probe) = &mut self.probe {
            if running {
                probe.poll();
            } else {
                probe.cancel();
            }
        }
    }

    /// PID du shell qui exécute la commande ; `None` si la session ne tourne pas.
    pub fn pid(&self) -> Option<u32> {
        self.run
            .as_ref()
            .filter(|run| run.pid != 0 && self.is_active())
            .map(|run| run.pid)
    }

    /// CPU, mémoire et ports de la session ; `None` si elle ne tourne pas.
    pub fn usage(&self) -> Option<Usage> {
        let run = self.run.as_ref().filter(|_| self.is_active())?;
        run.sampler.lock().unwrap().sample()
    }

    /// Ajuste la taille du PTY ; mémorisée pour les prochains démarrages.
    pub fn resize(&mut self, rows: u16, cols: u16) {
        if self.size == (rows, cols) {
            return;
        }
        self.size = (rows, cols);
        if let Some(run) = &self.run {
            let _ = run.master.lock().unwrap().resize(PtySize {
                rows,
                cols,
                pixel_width: 0,
                pixel_height: 0,
            });
        }
    }

    /// Variables de l'exécution en cours (ou dernière), à défaut de la définition courante.
    pub fn env(&self) -> Vec<(String, String)> {
        let env = self
            .run
            .as_ref()
            .map_or(&self.spec.env, |run| &run.spec.env);
        env.iter().map(|(k, v)| (k.clone(), v.clone())).collect()
    }

    /// Commande de l'exécution en cours (ou dernière), à défaut de la définition courante.
    pub fn command(&self) -> String {
        self.run
            .as_ref()
            .map_or(&self.spec.command, |run| &run.spec.command)
            .clone()
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
        session.cancel_and_stop();
    }
    let deadline = Instant::now() + STOP_TIMEOUT + Duration::from_secs(1);
    while Instant::now() < deadline && sessions.iter().any(Session::is_active) {
        thread::sleep(Duration::from_millis(50));
    }
}
