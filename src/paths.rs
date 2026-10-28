use std::fs::DirBuilder;
use std::os::unix::ffi::OsStrExt;
use std::os::unix::fs::{DirBuilderExt, PermissionsExt};
use std::path::{Path, PathBuf};

use anyhow::{Context, Result, bail};

/// Limite de `sun_path` sous Linux (108 octets, terminateur compris), avec une marge.
const MAX_SOCKET_PATH: usize = 100;

/// Socket du daemon d'un projet, identifié par le chemin absolu de son fichier de config.
/// Crée le dossier parent en `0700` : c'est lui qui protège la socket des autres utilisateurs.
pub fn socket_for(config_file: &Path) -> Result<PathBuf> {
    let dir = runtime_dir()?;
    let socket = dir.join(format!("{}.sock", project_id(config_file)));
    if socket.as_os_str().len() > MAX_SOCKET_PATH {
        bail!("chemin de socket trop long ({}) : raccourcir $XDG_RUNTIME_DIR", socket.display());
    }
    Ok(socket)
}

fn runtime_dir() -> Result<PathBuf> {
    let dir = match std::env::var_os("XDG_RUNTIME_DIR") {
        Some(runtime) => PathBuf::from(runtime).join("lazyshell"),
        None => PathBuf::from(format!("/tmp/lazyshell-{}", unsafe { libc::getuid() })),
    };
    DirBuilder::new().recursive(true).mode(0o700).create(&dir)
        .with_context(|| format!("création de {}", dir.display()))?;
    std::fs::set_permissions(&dir, std::fs::Permissions::from_mode(0o700))?;
    Ok(dir)
}

/// FNV-1a : stable d'une version de Rust à l'autre, contrairement à `DefaultHasher`.
fn fnv1a(bytes: &[u8]) -> u64 {
    bytes.iter().fold(0xcbf29ce484222325, |hash, &b| (hash ^ b as u64).wrapping_mul(0x100000001b3))
}

fn project_id(config_file: &Path) -> String {
    format!("{:016x}", fnv1a(config_file.as_os_str().as_encoded_bytes()))
}

/// Fichier de verrou du daemon : tenu tant qu'il tourne, il garantit qu'il est unique.
pub fn lock_for(socket: &Path) -> PathBuf {
    socket.with_extension("lock")
}

/// Fichier où le daemon note le chemin de sa config : le nom de la socket n'en est
/// qu'un hash, et `ps` doit pouvoir dire à quelle config appartient chaque daemon.
pub fn config_record_for(socket: &Path) -> PathBuf {
    socket.with_extension("config")
}

/// Daemons dont une socket existe : (fichier de config, socket). Une socket peut être
/// orpheline (daemon tué) ; c'est à l'appelant de s'y connecter pour le savoir.
pub fn known_daemons() -> Result<Vec<(PathBuf, PathBuf)>> {
    let mut daemons = Vec::new();
    for entry in std::fs::read_dir(runtime_dir()?)? {
        let socket = entry?.path();
        if socket.extension().is_none_or(|ext| ext != "sock") {
            continue;
        }
        // Sans fichier de config lisible (daemon d'une ancienne version), on l'ignore.
        let Ok(config) = std::fs::read(config_record_for(&socket)) else { continue };
        let config = PathBuf::from(std::ffi::OsStr::from_bytes(&config));
        daemons.push((config, socket));
    }
    daemons.sort();
    Ok(daemons)
}

/// Journal du daemon (sa sortie standard et d'erreur), dans le dossier d'état de l'utilisateur.
pub fn log_for(config_file: &Path) -> Result<PathBuf> {
    let state = match std::env::var_os("XDG_STATE_HOME") {
        Some(dir) => PathBuf::from(dir),
        None => PathBuf::from(std::env::var_os("HOME").context("$HOME non défini")?).join(".local/state"),
    };
    let dir = state.join("lazyshell");
    DirBuilder::new().recursive(true).mode(0o700).create(&dir)
        .with_context(|| format!("création de {}", dir.display()))?;
    Ok(dir.join(format!("{}.log", project_id(config_file))))
}
