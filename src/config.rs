use std::collections::{HashMap, HashSet};
use std::path::{Path, PathBuf};
use std::time::Duration;

use anyhow::{Context, Result, bail};
use serde::{Deserialize, Deserializer, de};

const DEFAULT_FILES: [&str; 2] = ["lazyshell.yml", "lazyshell.yaml"];
const DEFAULT_READY_TIMEOUT: Duration = Duration::from_secs(60);

#[derive(Debug, Deserialize)]
pub struct Config {
    #[serde(default)]
    pub groups: Vec<GroupConfig>,
    pub shell: Option<String>,
    /// Fichiers d'env (`.env`) appliqués à toutes les sessions.
    #[serde(default)]
    pub env_file: EnvFiles,
    pub sessions: Vec<SessionConfig>,
}

/// Un chemin ou une liste de chemins ; les suivants surchargent les précédents.
/// Un fichier absent est ignoré.
#[derive(Debug, Default, Deserialize)]
#[serde(untagged)]
pub enum EnvFiles {
    #[default]
    None,
    One(String),
    Many(Vec<String>),
}

impl EnvFiles {
    fn paths(&self) -> &[String] {
        match self {
            EnvFiles::None => &[],
            EnvFiles::One(path) => std::slice::from_ref(path),
            EnvFiles::Many(paths) => paths,
        }
    }
}

#[derive(Debug, Deserialize)]
pub struct GroupConfig {
    pub name: String,
}

/// Nature d'une session : détermine ce qu'attendent les sessions qui en dépendent.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Kind {
    /// Reste lancée : les dépendants attendent son lancement (ou sa sonde `ready`).
    #[default]
    Service,
    /// Se termine : les dépendants attendent le code de sortie 0.
    Task,
}

/// Sonde de disponibilité d'un service : `command` est rejouée jusqu'à réussir.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ReadyConfig {
    pub command: String,
    /// Délai, compté depuis le lancement, au-delà duquel le service est déclaré non prêt.
    #[serde(default = "default_ready_timeout", deserialize_with = "duration")]
    pub timeout: Duration,
}

fn default_ready_timeout() -> Duration {
    DEFAULT_READY_TIMEOUT
}

/// `30`, `30s` ou `2m` ; strictement positif.
fn duration<'de, D: Deserializer<'de>>(deserializer: D) -> Result<Duration, D::Error> {
    #[derive(Deserialize)]
    #[serde(untagged)]
    enum Raw {
        Seconds(u64),
        Text(String),
    }
    let (value, text) = match Raw::deserialize(deserializer)? {
        Raw::Seconds(seconds) => (Some(seconds), seconds.to_string()),
        Raw::Text(text) => {
            let (number, unit) = match text.strip_suffix('m') {
                Some(number) => (number, 60),
                None => (text.strip_suffix('s').unwrap_or(&text), 1),
            };
            let value = number.parse::<u64>().ok().and_then(|n| n.checked_mul(unit));
            (value, text)
        }
    };
    value
        .filter(|&seconds| seconds > 0 && seconds <= u64::from(u32::MAX))
        .map(Duration::from_secs)
        .ok_or_else(|| {
            de::Error::custom(format!(
                "durée invalide « {text} » (attendu : 30, 30s ou 2m)"
            ))
        })
}

#[derive(Debug, Deserialize)]
pub struct SessionConfig {
    pub name: String,
    pub group: Option<String>,
    pub cwd: Option<String>,
    pub command: String,
    /// Fichiers d'env propres à la session ; surchargent ceux de la racine.
    #[serde(default)]
    pub env_file: EnvFiles,
    /// Variables en ligne : surchargent tous les fichiers d'env.
    #[serde(default)]
    pub env: HashMap<String, String>,
    #[serde(default)]
    pub autostart: bool,
    #[serde(default, rename = "type")]
    pub kind: Kind,
    /// Sessions à lancer, et à attendre, avant celle-ci.
    #[serde(default)]
    pub depends_on: Vec<String>,
    pub ready: Option<ReadyConfig>,
}

impl Config {
    /// Trouve le fichier de config : le chemin lui-même, ou `lazyshell.yml` dans le dossier.
    /// Le chemin renvoyé est absolu : il identifie le projet (socket du daemon).
    pub fn resolve_file(path: &Path) -> Result<PathBuf> {
        let file = if path.is_dir() {
            Self::find_in_ancestors(path).with_context(|| {
                format!(
                    "fichier {} introuvable dans {} et ses parents",
                    DEFAULT_FILES.join(" / "),
                    path.display()
                )
            })?
        } else {
            path.to_path_buf()
        };
        file.canonicalize()
            .with_context(|| format!("fichier de config introuvable : {}", file.display()))
    }

    /// Cherche la config dans `dir`, puis dans ses parents jusqu'au dossier personnel (inclus).
    /// Si `dir` n'est pas sous le dossier personnel (ou s'il est inconnu), seul `dir` est examiné :
    /// on ne remonte jamais jusqu'à `/`.
    fn find_in_ancestors(dir: &Path) -> Option<PathBuf> {
        let dir = dir.canonicalize().ok()?;
        let home = std::env::var_os("HOME").and_then(|h| PathBuf::from(h).canonicalize().ok());
        let in_home = home.as_ref().is_some_and(|h| dir.starts_with(h));

        for current in dir.ancestors() {
            if let Some(found) = DEFAULT_FILES
                .iter()
                .map(|n| current.join(n))
                .find(|c| c.is_file())
            {
                return Some(found);
            }
            if !in_home || Some(current) == home.as_deref() {
                break;
            }
        }
        None
    }

    /// Charge la config. Retourne aussi le dossier de base servant à résoudre les `cwd` relatifs.
    pub fn load(path: &Path) -> Result<(Config, PathBuf)> {
        let file = Self::resolve_file(path)?;
        let text = std::fs::read_to_string(&file)
            .with_context(|| format!("lecture impossible : {}", file.display()))?;
        let mut config: Config = serde_yaml::from_str(&text)
            .with_context(|| format!("config invalide : {}", file.display()))?;
        config.validate()?;

        let base = file
            .parent()
            .map(Path::to_path_buf)
            .unwrap_or_else(|| PathBuf::from("."));
        config.resolve_env(&base)?;
        Ok((config, base))
    }

    /// Fusionne dans `env` de chaque session : fichiers racine < fichiers de la session < `env` en ligne.
    /// Les chemins relatifs partent du dossier de la config.
    fn resolve_env(&mut self, base: &Path) -> Result<()> {
        let mut global = HashMap::new();
        for path in self.env_file.paths() {
            global.extend(crate::envfile::load(&base.join(path))?);
        }
        for session in &mut self.sessions {
            let mut env = global.clone();
            for path in session.env_file.paths() {
                env.extend(crate::envfile::load(&base.join(path))?);
            }
            env.extend(std::mem::take(&mut session.env));
            session.env = env;
        }
        Ok(())
    }

    fn validate(&self) -> Result<()> {
        if self.sessions.is_empty() {
            bail!("aucune session définie dans la config");
        }
        let mut seen = HashSet::new();
        for session in &self.sessions {
            if !seen.insert(&session.name) {
                bail!("nom de session en double : {}", session.name);
            }
        }
        for session in &self.sessions {
            if session.kind == Kind::Task && session.ready.is_some() {
                bail!(
                    "{} : `ready` ne s'applique qu'à un service, pas à une session `type: task`",
                    session.name
                );
            }
            if let Some(unknown) = session.depends_on.iter().find(|d| !seen.contains(d)) {
                bail!(
                    "{} : dépendance inconnue dans `depends_on` : {unknown}",
                    session.name
                );
            }
        }
        self.check_cycles()
    }

    /// Refuse les dépendances circulaires, en citant la boucle trouvée.
    fn check_cycles(&self) -> Result<()> {
        fn visit<'a>(
            name: &'a str,
            deps: &HashMap<&'a str, &'a [String]>,
            path: &mut Vec<&'a str>,
            done: &mut HashSet<&'a str>,
        ) -> Result<()> {
            if let Some(start) = path.iter().position(|&n| n == name) {
                let mut cycle = path[start..].to_vec();
                cycle.push(name);
                bail!("dépendance circulaire : {}", cycle.join(" → "));
            }
            if done.contains(name) {
                return Ok(());
            }
            path.push(name);
            for dep in deps.get(name).copied().unwrap_or_default() {
                visit(dep, deps, path, done)?;
            }
            path.pop();
            done.insert(name);
            Ok(())
        }

        let deps: HashMap<&str, &[String]> = self
            .sessions
            .iter()
            .map(|s| (s.name.as_str(), s.depends_on.as_slice()))
            .collect();
        let mut done = HashSet::new();
        for session in &self.sessions {
            visit(&session.name, &deps, &mut Vec::new(), &mut done)?;
        }
        Ok(())
    }

    pub fn shell(&self) -> String {
        self.shell
            .clone()
            .or_else(|| std::env::var("SHELL").ok())
            .unwrap_or_else(|| "/bin/sh".to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(yaml: &str) -> Result<Config> {
        let config: Config = serde_yaml::from_str(yaml)?;
        config.validate()?;
        Ok(config)
    }

    fn error(yaml: &str) -> String {
        parse(yaml).unwrap_err().to_string()
    }

    #[test]
    fn dependencies_are_read_with_defaults() {
        let config = parse(
            "sessions:
  - {name: install, type: task, command: x}
  - {name: db, command: x, ready: {command: y}}
  - {name: api, command: x, depends_on: [install, db]}",
        )
        .unwrap();
        assert_eq!(config.sessions[0].kind, Kind::Task);
        assert_eq!(config.sessions[1].kind, Kind::Service);
        let ready = config.sessions[1].ready.as_ref().unwrap();
        assert_eq!(ready.timeout, Duration::from_secs(60));
        assert_eq!(config.sessions[2].depends_on, ["install", "db"]);
    }

    #[test]
    fn ready_timeout_formats() {
        let timeout = |value: &str| {
            parse(&format!(
                "sessions: [{{name: a, command: x, ready: {{command: y, timeout: {value}}}}}]"
            ))
            .map(|c| c.sessions[0].ready.as_ref().unwrap().timeout.as_secs())
        };
        assert_eq!(timeout("30").unwrap(), 30);
        assert_eq!(timeout("45s").unwrap(), 45);
        assert_eq!(timeout("2m").unwrap(), 120);
        for invalid in ["0", "0s", "-1", "1h", "abc", "''"] {
            assert!(timeout(invalid).is_err(), "{invalid}");
        }
    }

    #[test]
    fn invalid_dependencies_are_rejected() {
        assert!(
            error("sessions: [{name: a, command: x, depends_on: [b]}]")
                .contains("dépendance inconnue")
        );
        assert!(error("sessions: [{name: a, command: x, depends_on: [a]}]").contains("a → a"));
        let cycle = error(
            "sessions:
  - {name: a, command: x, depends_on: [b]}
  - {name: b, command: x, depends_on: [c]}
  - {name: c, command: x, depends_on: [b]}",
        );
        assert!(cycle.contains("b → c → b"), "{cycle}");
        assert!(
            error("sessions: [{name: a, type: task, command: x, ready: {command: y}}]")
                .contains("ready")
        );
    }

    #[test]
    fn shared_dependency_is_not_a_cycle() {
        parse(
            "sessions:
  - {name: web, command: x, depends_on: [install, api]}
  - {name: api, command: x, depends_on: [install]}
  - {name: install, command: x}",
        )
        .unwrap();
    }

    #[test]
    fn unknown_type_or_ready_key_is_rejected() {
        assert!(parse("sessions: [{name: a, command: x, type: job}]").is_err());
        assert!(
            parse("sessions: [{name: a, command: x, ready: {command: y, timout: 3}}]").is_err()
        );
    }
}
