use std::fs::{File, OpenOptions};
use std::io::{ErrorKind, Write};
use std::sync::mpsc::{Receiver, RecvTimeoutError};
use std::os::fd::AsRawFd;
use std::os::unix::fs::PermissionsExt;
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use anyhow::{Context, Result, bail};

use super::{Manager, Subscription};
use crate::config::Config;
use crate::paths;
use crate::protocol::{
    Request, Response, VERSION, read_handshake, read_message, write_handshake, write_message,
};

/// Supprime le fichier socket quand le daemon se termine, y compris sur erreur.
struct SocketGuard(PathBuf);

impl Drop for SocketGuard {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

/// Compte les connexions ouvertes ; décrémenté quand le thread du client se termine.
struct ClientGuard(Arc<AtomicUsize>);

impl ClientGuard {
    fn new(count: &Arc<AtomicUsize>) -> Self {
        count.fetch_add(1, Ordering::SeqCst);
        ClientGuard(Arc::clone(count))
    }
}

impl Drop for ClientGuard {
    fn drop(&mut self) {
        self.0.fetch_sub(1, Ordering::SeqCst);
    }
}

/// Inactivité (aucun client, aucune session active) au bout de laquelle un daemon
/// lancé en arrière-plan s'arrête.
const IDLE_EXIT: Duration = Duration::from_secs(10);

/// Fait tourner le daemon jusqu'à la commande `Shutdown` (ou, avec `auto_exit`,
/// jusqu'à `IDLE_EXIT` d'inactivité).
pub fn run(config: &Config, base: &Path, config_file: &Path, socket: &Path, auto_exit: bool) -> Result<()> {
    let _lock = acquire_lock(socket)?;
    let listener = bind(socket)?;
    let _guard = SocketGuard(socket.to_path_buf());
    let manager = Arc::new(Mutex::new(Manager::new(config, base)?));
    let stop = Arc::new(AtomicBool::new(false));
    let config_file = Arc::new(config_file.to_path_buf());
    install_signal_handlers();
    let clients = Arc::new(AtomicUsize::new(0));
    log(&format!("démarré (pid {}, socket {})", std::process::id(), socket.display()));

    listener.set_nonblocking(true)?;
    let mut idle_since: Option<Instant> = None;
    while !stop.load(Ordering::SeqCst) {
        if TERMINATED.load(Ordering::SeqCst) {
            log("signal d'arrêt reçu : arrêt des sessions");
            manager.lock().unwrap().shutdown();
            return Ok(());
        }
        match listener.accept() {
            Ok((stream, _)) => {
                let (manager, stop, config_file) =
                    (Arc::clone(&manager), Arc::clone(&stop), Arc::clone(&config_file));
                let client = ClientGuard::new(&clients);
                thread::spawn(move || {
                    let _client = client;
                    if let Err(err) = serve(stream, &manager, &stop, &config_file) {
                        // Un client qui part sans prévenir n'est pas une erreur du daemon.
                        let gone = err.downcast_ref::<std::io::Error>().is_some_and(|e| {
                            matches!(e.kind(), ErrorKind::BrokenPipe | ErrorKind::ConnectionReset)
                        });
                        if !gone {
                            log(&format!("client : {err:#}"));
                        }
                    }
                });
            }
            Err(err) if err.kind() == ErrorKind::WouldBlock => {}
            Err(err) => return Err(err.into()),
        }

        let busy = {
            let mut manager = manager.lock().unwrap();
            manager.tick();
            manager.has_active()
        } || clients.load(Ordering::SeqCst) > 0;
        if auto_exit {
            if busy {
                idle_since = None;
            } else if idle_since.get_or_insert_with(Instant::now).elapsed() >= IDLE_EXIT {
                log(&format!("inactif depuis {} s : arrêt", IDLE_EXIT.as_secs()));
                return Ok(());
            }
        }
        thread::sleep(Duration::from_millis(50));
    }
    log("arrêt demandé");
    Ok(())
}

/// Posé par le gestionnaire de signal : SIGTERM/SIGINT demandent un arrêt propre.
static TERMINATED: AtomicBool = AtomicBool::new(false);

extern "C" fn on_terminate(_: libc::c_int) {
    TERMINATED.store(true, Ordering::SeqCst);
}

fn install_signal_handlers() {
    unsafe {
        libc::signal(libc::SIGTERM, on_terminate as *const () as libc::sighandler_t);
        libc::signal(libc::SIGINT, on_terminate as *const () as libc::sighandler_t);
    }
}

/// Ligne de journal horodatée (secondes depuis l'epoch) sur la sortie d'erreur.
fn log(message: &str) {
    let now = SystemTime::now().duration_since(UNIX_EPOCH).map_or(0, |d| d.as_secs());
    eprintln!("[{now}] {message}");
}

/// Verrou exclusif tenu pendant toute la vie du daemon : un seul par projet, et tout
/// fichier socket trouvé ensuite est forcément orphelin.
fn acquire_lock(socket: &Path) -> Result<File> {
    let path = paths::lock_for(socket);
    let file = OpenOptions::new().create(true).truncate(false).write(true).open(&path)
        .with_context(|| format!("verrou {}", path.display()))?;
    if unsafe { libc::flock(file.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) } != 0 {
        bail!("un daemon tourne déjà pour ce projet ({})", socket.display());
    }
    // Le pid sert à désigner un daemon d'une autre version qu'on ne peut plus piloter.
    file.set_len(0)?;
    (&file).write_all(std::process::id().to_string().as_bytes())?;
    Ok(file)
}

/// Ouvre la socket (le verrou est tenu : un fichier existant est un reste à supprimer).
fn bind(socket: &Path) -> Result<UnixListener> {
    let _ = std::fs::remove_file(socket);
    let listener =
        UnixListener::bind(socket).with_context(|| format!("socket {}", socket.display()))?;
    std::fs::set_permissions(socket, std::fs::Permissions::from_mode(0o600))?;
    Ok(listener)
}

fn serve(mut stream: UnixStream, manager: &Mutex<Manager>, stop: &AtomicBool, config_file: &Path) -> Result<()> {
    stream.set_nonblocking(false)?;
    // Une connexion ouverte puis fermée sans rien dire (test de présence) n'est pas une erreur.
    let client_version = match read_handshake(&mut stream) {
        Ok(version) => version,
        Err(err) if err.downcast_ref::<std::io::Error>().is_some_and(|e| e.kind() == ErrorKind::UnexpectedEof) => {
            return Ok(());
        }
        Err(err) => return Err(err),
    };
    write_handshake(&mut stream)?;
    if client_version != VERSION {
        return Ok(());
    }

    while let Some(request) = read_message::<Request>(&mut stream)? {
        let response = match request {
            Request::List => Response::Infos(manager.lock().unwrap().infos()),
            Request::Command(command) => match manager.lock().unwrap().command(command) {
                Ok(()) => Response::Ok,
                Err(err) => Response::Error(err.to_string()),
            },
            Request::Resize { session, rows, cols } => {
                match manager.lock().unwrap().resize(&session, rows, cols) {
                    Ok(()) => Response::Ok,
                    Err(err) => Response::Error(err.to_string()),
                }
            }
            Request::Subscribe { session } => {
                let subscription = manager.lock().unwrap().subscribe(&session);
                match subscription {
                    // La connexion devient un flux : on n'en revient pas.
                    Ok(subscription) => return push_stream(stream, subscription),
                    Err(err) => Response::Error(err.to_string()),
                }
            }
            Request::Reload => {
                let reloaded = Config::load(config_file)
                    .and_then(|(config, base)| manager.lock().unwrap().reload(&config, &base));
                match reloaded {
                    Ok(report) => Response::Reloaded(report),
                    Err(err) => Response::Error(format!("{err:#}")),
                }
            }
            Request::Shutdown => {
                manager.lock().unwrap().shutdown();
                write_message(&mut stream, &Response::Ok)?;
                stop.store(true, Ordering::SeqCst);
                return Ok(());
            }
        };
        write_message(&mut stream, &response)?;
    }
    Ok(())
}

/// Pousse l'historique puis la sortie en direct jusqu'à la fin de l'exécution.
fn push_stream(mut stream: UnixStream, subscription: Subscription) -> Result<()> {
    write_message(&mut stream, &Response::Subscribed { run_id: subscription.run_id })?;
    write_message(&mut stream, &Response::Output(subscription.replay))?;
    forward(&mut stream, &subscription.live)
}

fn forward(stream: &mut UnixStream, live: &Receiver<Vec<u8>>) -> Result<()> {
    loop {
        match live.recv_timeout(Duration::from_secs(1)) {
            Ok(mut chunk) => {
                // On regroupe ce qui est déjà arrivé pour limiter le nombre de trames.
                while chunk.len() < 64 * 1024 {
                    match live.try_recv() {
                        Ok(more) => chunk.extend_from_slice(&more),
                        Err(_) => break,
                    }
                }
                write_message(stream, &Response::Output(chunk))?;
            }
            Err(RecvTimeoutError::Timeout) => write_message(stream, &Response::Ping)?,
            Err(RecvTimeoutError::Disconnected) => {
                write_message(stream, &Response::StreamEnd)?;
                return Ok(());
            }
        }
    }
}
