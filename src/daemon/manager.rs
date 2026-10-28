use std::path::Path;
use std::time::Duration;

use anyhow::{Result, anyhow};
use serde::{Deserialize, Serialize};

use super::session::{Session, Status, Subscription, shutdown};
use crate::config::Config;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum Target {
    Session(String),
    Group(String),
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum Command {
    Start(Target),
    Stop(Target),
    Restart(Target),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum State {
    NotStarted,
    Running,
    Stopping,
    Exited(i32),
}

/// Vue d'une session sans accès au processus : ce que la TUI a le droit de savoir.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SessionInfo {
    pub name: String,
    pub group: String,
    pub state: State,
    pub uptime: Duration,
    /// Change à chaque (re)démarrage ; `None` tant que la session n'a jamais tourné.
    pub run_id: Option<u64>,
    /// La config a changé depuis le lancement : un redémarrage appliquera la nouvelle.
    pub outdated: bool,
}

/// Bilan d'un rechargement de la config.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ReloadReport {
    pub added: Vec<String>,
    pub removed: Vec<String>,
    /// Définition modifiée : prise en compte au prochain démarrage.
    pub changed: Vec<String>,
}

pub struct Manager {
    sessions: Vec<Session>,
    declared_groups: Vec<String>,
    /// Indices dans `sessions`, regroupés dans l'ordre d'affichage.
    order: Vec<usize>,
}

/// Groupes déclarés d'abord, puis ceux rencontrés uniquement dans les sessions ;
/// les sessions sont rangées groupe par groupe.
fn arrange(declared: &[String], sessions: &[Session]) -> Vec<usize> {
    let mut groups = declared.to_vec();
    for session in sessions {
        if !groups.contains(&session.group) {
            groups.push(session.group.clone());
        }
    }
    groups
        .iter()
        .flat_map(|group| {
            sessions.iter().enumerate().filter(move |(_, s)| &s.group == group).map(|(i, _)| i)
        })
        .collect()
}

impl Manager {
    pub fn new(config: &Config, base: &Path) -> Result<Self> {
        let shell = config.shell();
        let mut sessions: Vec<Session> =
            config.sessions.iter().map(|cfg| Session::new(cfg, base, &shell)).collect();
        let declared_groups: Vec<String> = config.groups.iter().map(|g| g.name.clone()).collect();
        let order = arrange(&declared_groups, &sessions);

        for (session, cfg) in sessions.iter_mut().zip(&config.sessions) {
            if cfg.autostart {
                session.start()?;
            }
        }
        Ok(Manager { sessions, declared_groups, order })
    }

    /// Applique une config relue. Les sessions inchangées ne sont pas touchées ; une
    /// session en cours dont la définition change garde l'ancienne jusqu'à son redémarrage.
    /// Refuse (sans rien modifier) de supprimer une session encore active.
    pub fn reload(&mut self, config: &Config, base: &Path) -> Result<ReloadReport> {
        let names: Vec<&str> = config.sessions.iter().map(|c| c.name.as_str()).collect();
        let blocking: Vec<&str> = self
            .sessions
            .iter()
            .filter(|s| !names.contains(&s.name.as_str()) && s.is_active())
            .map(|s| s.name.as_str())
            .collect();
        if !blocking.is_empty() {
            return Err(anyhow!(
                "sessions retirées de la config mais encore actives : {} (les arrêter d'abord)",
                blocking.join(", ")
            ));
        }

        let shell = config.shell();
        let mut report = ReloadReport::default();
        report.removed =
            self.sessions.iter().filter(|s| !names.contains(&s.name.as_str())).map(|s| s.name.clone()).collect();
        self.sessions.retain(|s| names.contains(&s.name.as_str()));

        for cfg in &config.sessions {
            match self.sessions.iter_mut().find(|s| s.name == cfg.name) {
                Some(session) => {
                    if session.update(cfg, base, &shell) {
                        report.changed.push(cfg.name.clone());
                    }
                }
                None => {
                    let mut session = Session::new(cfg, base, &shell);
                    if cfg.autostart {
                        session.start()?;
                    }
                    self.sessions.push(session);
                    report.added.push(cfg.name.clone());
                }
            }
        }
        self.declared_groups = config.groups.iter().map(|g| g.name.clone()).collect();
        self.order = arrange(&self.declared_groups, &self.sessions);
        Ok(report)
    }

    /// Sessions dans l'ordre d'affichage (regroupées).
    pub fn infos(&self) -> Vec<SessionInfo> {
        self.order
            .iter()
            .map(|&index| {
                let session = &self.sessions[index];
                let state = match session.status() {
                    None => State::NotStarted,
                    Some(Status::Running) => State::Running,
                    Some(Status::Stopping) => State::Stopping,
                    Some(Status::Exited(code)) => State::Exited(code),
                };
                SessionInfo {
                    name: session.name.clone(),
                    group: session.group.clone(),
                    state,
                    uptime: session.run.as_ref().map_or(Duration::ZERO, |r| r.started_at.elapsed()),
                    run_id: session.run.as_ref().map(|r| r.id),
                    outdated: session.outdated,
                }
            })
            .collect()
    }

    /// Au moins une session tourne (ou est en cours d'arrêt).
    pub fn has_active(&self) -> bool {
        self.sessions.iter().any(Session::is_active)
    }

    /// Finalise les redémarrages en attente ; à appeler régulièrement.
    pub fn tick(&mut self) {
        for session in &mut self.sessions {
            session.tick();
        }
    }

    /// Applique la commande à toutes les sessions visées ; renvoie la première erreur.
    pub fn command(&mut self, command: Command) -> Result<()> {
        let (target, action): (_, fn(&mut Session) -> Result<()>) = match command {
            Command::Start(target) => (target, Session::start),
            Command::Stop(target) => (target, |s| {
                s.stop();
                Ok(())
            }),
            Command::Restart(target) => (target, Session::restart),
        };

        let mut first_error = None;
        let mut matched = false;
        for session in &mut self.sessions {
            let hit = match &target {
                Target::Session(name) => &session.name == name,
                Target::Group(group) => &session.group == group,
            };
            if !hit {
                continue;
            }
            matched = true;
            if let Err(err) = action(session) {
                first_error.get_or_insert_with(|| anyhow!("{} : {err}", session.name));
            }
        }
        match (first_error, matched) {
            (Some(err), _) => Err(err),
            (None, false) => Err(anyhow!("cible inconnue")),
            (None, true) => Ok(()),
        }
    }

    /// Historique puis flux en direct de la sortie de la session.
    pub fn subscribe(&self, name: &str) -> Result<Subscription> {
        let session = self.find(name)?;
        session.subscribe().ok_or_else(|| anyhow!("{name} n'a pas encore été lancée"))
    }

    pub fn resize(&mut self, name: &str, rows: u16, cols: u16) -> Result<()> {
        let session = self
            .sessions
            .iter_mut()
            .find(|s| s.name == name)
            .ok_or_else(|| anyhow!("session inconnue : {name}"))?;
        session.resize(rows.max(1), cols.max(1));
        Ok(())
    }

    fn find(&self, name: &str) -> Result<&Session> {
        self.sessions.iter().find(|s| s.name == name).ok_or_else(|| anyhow!("session inconnue : {name}"))
    }

    /// Arrête toutes les sessions actives et attend leur fin.
    pub fn shutdown(&mut self) {
        shutdown(&mut self.sessions);
    }
}

impl ReloadReport {
    pub fn summary(&self) -> String {
        let parts: Vec<String> = [
            ("ajoutées", &self.added),
            ("retirées", &self.removed),
            ("modifiées (effet au prochain démarrage)", &self.changed),
        ]
        .into_iter()
        .filter(|(_, names)| !names.is_empty())
        .map(|(label, names)| format!("{label} : {}", names.join(", ")))
        .collect();
        if parts.is_empty() { "aucun changement".to_string() } else { parts.join(" ; ") }
    }
}
