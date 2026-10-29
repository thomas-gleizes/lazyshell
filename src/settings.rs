use std::path::PathBuf;

use anyhow::{Context, Result, bail};
use serde::Deserialize;

use crate::i18n::Lang;

/// Réglages globaux de l'utilisateur (par opposition au `lazyshell.yml` d'un projet).
#[derive(Debug, Default, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Settings {
    #[serde(default)]
    pub language: Lang,
}

impl Settings {
    /// Charge `config.yaml` du dossier de config utilisateur. Un fichier absent donne les
    /// réglages par défaut ; un fichier invalide est une erreur, pour ne pas ignorer une faute de frappe.
    pub fn load() -> Result<Settings> {
        let file = config_file()?;
        let text = match std::fs::read_to_string(&file) {
            Ok(text) => text,
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(Settings::default()),
            Err(err) => return Err(err).with_context(|| format!("lecture de {}", file.display())),
        };
        // Un fichier vide (ou que des commentaires) est valide : serde_yaml le lit comme `null`.
        if text.trim().is_empty() {
            return Ok(Settings::default());
        }
        serde_yaml::from_str(&text).with_context(|| format!("réglages invalides dans {}", file.display()))
    }
}

/// `$XDG_CONFIG_HOME/lazyshell/config.yaml`, sinon `~/.config/lazyshell/config.yaml`.
/// Même convention sur toutes les distros Linux (et sur macOS, où les outils en ligne de commande
/// utilisent couramment `~/.config`).
pub fn config_file() -> Result<PathBuf> {
    let base = match std::env::var_os("XDG_CONFIG_HOME").filter(|dir| !dir.is_empty()) {
        Some(dir) => PathBuf::from(dir),
        None => match std::env::var_os("HOME").filter(|dir| !dir.is_empty()) {
            Some(home) => PathBuf::from(home).join(".config"),
            None => bail!("$HOME non défini : impossible de trouver ~/.config/lazyshell/config.yaml"),
        },
    };
    Ok(base.join("lazyshell").join("config.yaml"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn language_defaults_to_french() {
        assert_eq!(serde_yaml::from_str::<Settings>("{}").unwrap().language, Lang::Fr);
    }

    #[test]
    fn language_is_read() {
        assert_eq!(serde_yaml::from_str::<Settings>("language: en").unwrap().language, Lang::En);
        assert_eq!(serde_yaml::from_str::<Settings>("language: fr").unwrap().language, Lang::Fr);
    }

    #[test]
    fn unknown_language_or_key_is_rejected() {
        assert!(serde_yaml::from_str::<Settings>("language: de").is_err());
        assert!(serde_yaml::from_str::<Settings>("langage: en").is_err());
    }
}
