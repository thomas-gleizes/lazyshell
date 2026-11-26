use std::path::Path;
use std::time::Duration;

use anyhow::{Result, anyhow};
use serde::{Deserialize, Serialize};

use super::schedule::Wait;
use super::session::{Session, Status, Subscription, shutdown};
use super::usage::Usage;
use crate::config::Config;
use crate::i18n;

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
    /// Démarrage en attente des dépendances.
    Waiting,
    /// Démarrage impossible tant qu'une dépendance reste en échec.
    Blocked,
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
    /// PID du shell de la session ; `None` tant qu'elle ne tourne pas.
    pub pid: Option<u32>,
    /// CPU et mémoire ; `None` tant que la session ne tourne pas.
    pub usage: Option<Usage>,
    /// Commande lancée par la session (celle de l'exécution en cours ou dernière).
    pub command: String,
    /// Ce qui retient le démarrage ; présent pour les états `Waiting` et `Blocked`.
    pub wait: Option<Wait>,
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
    pub(super) sessions: Vec<Session>,
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
            sessions
                .iter()
                .enumerate()
                .filter(move |(_, s)| &s.group == group)
                .map(|(i, _)| i)
        })
        .collect()
}

impl Manager {
    pub fn new(config: &Config, base: &Path) -> Self {
        let shell = config.shell();
        let sessions: Vec<Session> = config
            .sessions
            .iter()
            .map(|cfg| Session::new(cfg, base, &shell))
            .collect();
        let declared_groups: Vec<String> = config.groups.iter().map(|g| g.name.clone()).collect();
        let order = arrange(&declared_groups, &sessions);

        let mut manager = Manager {
            sessions,
            declared_groups,
            order,
        };
        let autostart: Vec<usize> = config
            .sessions
            .iter()
            .enumerate()
            .filter(|(_, cfg)| cfg.autostart)
            .map(|(index, _)| index)
            .collect();
        // Un échec de lancement n'empêche pas le daemon de servir les autres sessions :
        // il reste visible dans l'état de la session (`Blocked`).
        let _ = manager.request(&autostart);
        manager
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
                "rechargement refusé : sessions absentes de la config mais actives : {}",
                blocking.join(", ")
            ));
        }

        let shell = config.shell();
        let mut report = ReloadReport {
            removed: self
                .sessions
                .iter()
                .filter(|s| !names.contains(&s.name.as_str()))
                .map(|s| s.name.clone())
                .collect(),
            ..Default::default()
        };
        self.sessions.retain(|s| names.contains(&s.name.as_str()));

        // Sessions à (re)planifier : nouvelles en `autostart`, ou en attente et recâblées.
        let mut to_request = Vec::new();
        for cfg in &config.sessions {
            match self.sessions.iter_mut().find(|s| s.name == cfg.name) {
                Some(session) => {
                    let rewired = session.depends_on != cfg.depends_on;
                    if rewired && session.wait.is_some() {
                        to_request.push(cfg.name.clone());
                    }
                    if session.update(cfg, base, &shell) {
                        report.changed.push(cfg.name.clone());
                    }
                }
                None => {
                    if cfg.autostart {
                        to_request.push(cfg.name.clone());
                    }
                    self.sessions.push(Session::new(cfg, base, &shell));
                    report.added.push(cfg.name.clone());
                }
            }
        }
        self.declared_groups = config.groups.iter().map(|g| g.name.clone()).collect();
        self.order = arrange(&self.declared_groups, &self.sessions);
        let to_request: Vec<usize> = to_request
            .iter()
            .filter_map(|name| self.index_of(name))
            .collect();
        // La config est appliquée : un échec de lancement ne la fait pas refuser.
        let _ = self.request(&to_request);
        Ok(report)
    }

    /// Sessions dans l'ordre d'affichage (regroupées).
    pub fn infos(&self) -> Vec<SessionInfo> {
        self.order
            .iter()
            .map(|&index| {
                let session = &self.sessions[index];
                let state = match (&session.wait, session.status()) {
                    (Some(Wait::On(_)), _) => State::Waiting,
                    (Some(_), _) => State::Blocked,
                    (None, None) => State::NotStarted,
                    (None, Some(Status::Running)) => State::Running,
                    (None, Some(Status::Stopping)) => State::Stopping,
                    (None, Some(Status::Exited(code))) => State::Exited(code),
                };
                SessionInfo {
                    name: session.name.clone(),
                    group: session.group.clone(),
                    state,
                    uptime: session
                        .run
                        .as_ref()
                        .map_or(Duration::ZERO, |r| r.started_at.elapsed()),
                    run_id: session.run.as_ref().map(|r| r.id),
                    outdated: session.outdated,
                    pid: session.pid(),
                    usage: session.usage(),
                    command: session.command(),
                    wait: session.wait.clone(),
                }
            })
            .collect()
    }

    /// Au moins une session tourne, s'arrête ou attend ses dépendances.
    pub fn has_active(&self) -> bool {
        self.sessions
            .iter()
            .any(|s| s.is_active() || matches!(s.wait, Some(Wait::On(_))))
    }

    /// Fait avancer sondes, redémarrages et attentes ; à appeler régulièrement.
    pub fn tick(&mut self) {
        for session in &mut self.sessions {
            session.tick();
        }
        let restarts: Vec<usize> = (0..self.sessions.len())
            .filter(|&i| self.sessions[i].take_restart())
            .collect();
        // Un échec de lancement reste visible dans l'état de la session (`Blocked`).
        let _ = self.request(&restarts);
    }

    /// Applique la commande à toutes les sessions visées ; renvoie la première erreur.
    pub fn command(&mut self, command: Command) -> Result<()> {
        let target = match &command {
            Command::Start(target) | Command::Stop(target) | Command::Restart(target) => target,
        };
        let hits: Vec<usize> = (0..self.sessions.len())
            .filter(|&i| match target {
                Target::Session(name) => &self.sessions[i].name == name,
                Target::Group(group) => &self.sessions[i].group == group,
            })
            .collect();
        if hits.is_empty() {
            return Err(anyhow!("cible inconnue"));
        }
        match command {
            Command::Start(_) => self.request(&hits),
            Command::Stop(_) => {
                for index in hits {
                    self.sessions[index].cancel_and_stop();
                }
                // Les sessions qui attendaient celles-ci sont bloquées sans attendre `tick`.
                self.request(&[])
            }
            Command::Restart(_) => {
                // Les actives s'arrêtent d'abord : les autres voient ainsi leurs dépendances
                // en cours de redémarrage et les attendent.
                let (active, idle): (Vec<usize>, Vec<usize>) = hits
                    .into_iter()
                    .partition(|&i| self.sessions[i].is_active());
                for index in active {
                    self.sessions[index].request_restart();
                }
                self.request(&idle)
            }
        }
    }

    /// Historique puis flux en direct de la sortie de la session.
    pub fn subscribe(&self, name: &str) -> Result<Subscription> {
        let session = self.find(name)?;
        session
            .subscribe()
            .ok_or_else(|| anyhow!("{name} : aucune exécution"))
    }

    /// Variables injectées dans la session (celles de l'exécution en cours, sinon de la définition).
    pub fn env(&self, name: &str) -> Result<Vec<(String, String)>> {
        Ok(self.find(name)?.env())
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

    pub(super) fn index_of(&self, name: &str) -> Option<usize> {
        self.sessions.iter().position(|s| s.name == name)
    }

    fn find(&self, name: &str) -> Result<&Session> {
        self.sessions
            .iter()
            .find(|s| s.name == name)
            .ok_or_else(|| anyhow!("session inconnue : {name}"))
    }

    /// Arrête toutes les sessions actives et attend leur fin.
    pub fn shutdown(&mut self) {
        shutdown(&mut self.sessions);
    }
}

impl ReloadReport {
    pub fn summary(&self) -> String {
        let parts: Vec<String> = [
            (i18n::reload_added(), &self.added),
            (i18n::reload_removed(), &self.removed),
            (i18n::reload_changed(), &self.changed),
        ]
        .into_iter()
        .filter(|(_, names)| !names.is_empty())
        .map(|(label, names)| format!("{label} : {}", names.join(", ")))
        .collect();
        if parts.is_empty() {
            i18n::reload_no_change().to_string()
        } else {
            parts.join(" ; ")
        }
    }
}
