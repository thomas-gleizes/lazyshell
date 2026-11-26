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
/// Durée au-delà de laquelle un essai est abandonné : un essai suspendu ne doit pas
/// consommer tout le délai de la sonde.
const ATTEMPT_LIMIT: Duration = Duration::from_secs(10);

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
    attempt_limit: Duration,
    /// Échéance de l'essai en cours ; sans objet sans essai.
    attempt_deadline: Instant,
    readiness: Readiness,
}

impl Probe {
    pub fn new(config: ReadyConfig, context: Context) -> Self {
        let now = Instant::now();
        Probe {
            deadline: now + config.timeout,
            next_attempt: now,
            attempt: None,
            attempt_limit: ATTEMPT_LIMIT,
            attempt_deadline: now,
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
                Ok(None) if Instant::now() >= self.attempt_deadline => {
                    self.cancel();
                    eprintln!(
                        "essai de sonde abandonné après {} s : {}",
                        self.attempt_limit.as_secs(),
                        self.config.command
                    );
                    self.next_attempt = Instant::now() + RETRY_EVERY;
                }
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
                Ok(child) => {
                    self.attempt = Some(child);
                    self.attempt_deadline = Instant::now() + self.attempt_limit;
                }
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

#[cfg(test)]
mod tests {
    use std::thread;

    use super::*;

    #[test]
    fn hung_attempt_is_abandoned_then_retried() {
        let dir = std::env::temp_dir().join(format!("lazyshell-probe-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        // Seul le premier essai reste suspendu.
        let config = ReadyConfig {
            command: "[ -f tried ] && exit 0; touch tried; sleep 30".into(),
            timeout: Duration::from_secs(60),
        };
        let context = Context {
            shell: "/bin/sh".into(),
            cwd: dir.clone(),
            env: BTreeMap::new(),
        };
        let mut probe = Probe::new(config, context);
        probe.attempt_limit = Duration::from_millis(300);

        let deadline = Instant::now() + Duration::from_secs(10);
        while probe.readiness() == Readiness::Probing && Instant::now() < deadline {
            probe.poll();
            thread::sleep(Duration::from_millis(10));
        }
        assert_eq!(probe.readiness(), Readiness::Ready);
        let _ = std::fs::remove_dir_all(&dir);
    }
}
