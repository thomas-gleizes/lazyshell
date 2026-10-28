use std::fs::OpenOptions;
use std::os::unix::net::UnixStream;
use std::os::unix::process::CommandExt;
use std::path::Path;
use std::process::{Command, ExitStatus, Stdio};
use std::thread;
use std::time::{Duration, Instant};

use anyhow::{Context, Result, bail};

use crate::config::Config;
use crate::paths;

const START_TIMEOUT: Duration = Duration::from_secs(5);
/// Si le daemon s'est déjà arrêté (verrou perdu, erreur), on n'attend pas tout le délai.
const EXITED_GRACE: Duration = Duration::from_secs(1);

/// S'assure qu'un daemon répond pour ce projet, en lançant au besoin un daemon détaché
/// du terminal (nouvelle session) qui s'arrêtera seul quand il n'aura plus rien à faire.
pub fn ensure_daemon(config_file: &Path, socket: &Path) -> Result<()> {
    if is_up(socket) {
        return Ok(());
    }
    // Une config invalide est signalée ici, dans le terminal, et non dans le journal.
    Config::load(config_file)?;

    let log_path = paths::log_for(config_file)?;
    let log = OpenOptions::new().create(true).append(true).open(&log_path)
        .with_context(|| format!("journal {}", log_path.display()))?;
    let mut command = Command::new(std::env::current_exe()?);
    command
        .args(["daemon", "--auto-exit"])
        .arg(config_file)
        .stdin(Stdio::null())
        .stdout(log.try_clone()?)
        .stderr(log);
    // Nouvelle session : le daemon survit à la fermeture du terminal (pas de SIGHUP).
    unsafe {
        command.pre_exec(|| {
            libc::setsid();
            Ok(())
        });
    }
    let mut child = command.spawn().context("lancement du daemon")?;

    let mut deadline = Instant::now() + START_TIMEOUT;
    let mut exited: Option<ExitStatus> = None;
    loop {
        if is_up(socket) {
            return Ok(());
        }
        if exited.is_none() {
            exited = child.try_wait()?;
            if exited.is_some() {
                deadline = deadline.min(Instant::now() + EXITED_GRACE);
            }
        }
        if Instant::now() >= deadline {
            let why = match exited {
                Some(status) => format!("le daemon s'est arrêté ({status})"),
                None => "le daemon ne répond pas".to_string(),
            };
            bail!("{why} ; voir {}", log_path.display());
        }
        thread::sleep(Duration::from_millis(50));
    }
}

fn is_up(socket: &Path) -> bool {
    UnixStream::connect(socket).is_ok()
}
