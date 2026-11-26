//! Sonde de disponibilité d'un service : sa commande est rejouée jusqu'à réussir ou
//! jusqu'à expiration du délai. Rien ne bloque : [`Probe::poll`] est appelée régulièrement.

use std::collections::BTreeMap;
use std::os::unix::process::CommandExt;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use crate::config::ReadyConfig;

/// Pause entre la fin d'un essai raté et le suivant.
const RETRY_EVERY: Duration = Duration::from_secs(1);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Readiness {
    Probing,
    Ready,
    TimedOut,
}

/// Contexte d'exécution des essais : celui de la session sondée.
#[derive(Clone)]
pub struct Context {
    pub shell: String,
    pub cwd: PathBuf,
    pub env: BTreeMap<String, String>,
}

/// Sonde d'une exécution ; abandonnée (processus tué) avec elle.
pub struct Probe {
    config: ReadyConfig,
    context: Context,
    deadline: Instant,
    next_attempt: Instant,
    attempt: Option<Child>,
    readiness: Readiness,
}

impl Probe {
    pub fn new(config: ReadyConfig, context: Context) -> Self {
        let now = Instant::now();
        Probe {
            deadline: now + config.timeout,
            next_attempt: now,
            attempt: None,
            readiness: Readiness::Probing,
            config,
            context,
        }
    }

    pub fn readiness(&self) -> Readiness {
        self.readiness
    }

    /// Relance une sonde expirée avec un nouveau délai ; sans effet sinon.
    pub fn rearm(&mut self) {
        if self.readiness == Readiness::TimedOut {
            let now = Instant::now();
            self.deadline = now + self.config.timeout;
            self.next_attempt = now;
            self.readiness = Readiness::Probing;
        }
    }

    /// Fait avancer la sonde : relève l'essai en cours ou en lance un nouveau.
    pub fn poll(&mut self) {
        if self.readiness != Readiness::Probing {
            return;
        }
        if let Some(child) = &mut self.attempt {
            match child.try_wait() {
                Ok(None) => {}
                Ok(Some(status)) => {
                    self.attempt = None;
                    if status.success() {
                        self.readiness = Readiness::Ready;
                        return;
                    }
                    self.next_attempt = Instant::now() + RETRY_EVERY;
                }
                Err(_) => self.cancel(),
            }
        }
        if Instant::now() >= self.deadline {
            self.cancel();
            self.readiness = Readiness::TimedOut;
            eprintln!(
                "sonde expirée après {} s : {}",
                self.config.timeout.as_secs(),
                self.config.command
            );
        } else if self.attempt.is_none() && Instant::now() >= self.next_attempt {
            match self.spawn() {
                Ok(child) => self.attempt = Some(child),
                Err(err) => {
                    eprintln!("sonde impossible à lancer : {err}");
                    self.next_attempt = Instant::now() + RETRY_EVERY;
                }
            }
        }
    }

    /// Tue l'essai en cours et tous ses descendants.
    pub fn cancel(&mut self) {
        let Some(mut child) = self.attempt.take() else {
            return;
        };
        // L'essai est chef de son groupe de processus : pgid == pid. Il n'est pas encore
        // récolté, son pid ne peut donc pas avoir été réattribué.
        unsafe {
            libc::kill(-(child.id() as i32), libc::SIGKILL);
        }
        let _ = child.wait();
    }

    fn spawn(&self) -> std::io::Result<Child> {
        Command::new(&self.context.shell)
            .args(["-c", &self.config.command])
            .current_dir(&self.context.cwd)
            .env("TERM", "xterm-256color")
            .envs(&self.context.env)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .process_group(0)
            .spawn()
    }
}

impl Drop for Probe {
    fn drop(&mut self) {
        self.cancel();
    }
}
