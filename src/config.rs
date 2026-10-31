use std::collections::HashMap;
use std::path::{Path, PathBuf};

use anyhow::{Context, Result, bail};
use serde::Deserialize;

const DEFAULT_FILES: [&str; 2] = ["lazyshell.yml", "lazyshell.yaml"];

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
}

impl Config {
    /// Trouve le fichier de config : le chemin lui-même, ou `lazyshell.yml` dans le dossier.
    /// Le chemin renvoyé est absolu : il identifie le projet (socket du daemon).
    pub fn resolve_file(path: &Path) -> Result<PathBuf> {
        let file = if path.is_dir() {
            DEFAULT_FILES
                .iter()
                .map(|name| path.join(name))
                .find(|candidate| candidate.is_file())
                .with_context(|| {
                    format!("aucun fichier {} dans {}", DEFAULT_FILES.join(" / "), path.display())
                })?
        } else {
            path.to_path_buf()
        };
        file.canonicalize().with_context(|| format!("config introuvable : {}", file.display()))
    }

    /// Charge la config. Retourne aussi le dossier de base servant à résoudre les `cwd` relatifs.
    pub fn load(path: &Path) -> Result<(Config, PathBuf)> {
        let file = Self::resolve_file(path)?;
        let text = std::fs::read_to_string(&file)
            .with_context(|| format!("lecture de {}", file.display()))?;
        let mut config: Config = serde_yaml::from_str(&text)
            .with_context(|| format!("config invalide dans {}", file.display()))?;
        config.validate()?;

        let base = file.parent().map(Path::to_path_buf).unwrap_or_else(|| PathBuf::from("."));
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
            bail!("aucune session définie");
        }
        let mut seen = std::collections::HashSet::new();
        for session in &self.sessions {
            if !seen.insert(&session.name) {
                bail!("nom de session en double : {}", session.name);
            }
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
