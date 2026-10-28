//! Messages échangés entre la TUI (ou la CLI) et le daemon : trames préfixées par
//! leur longueur (u32 big-endian), charge utile en postcard (binaire compact, pour
//! que les octets de sortie ne soient pas gonflés). Seul ce module connaît le codage.
//!
//! Deux sortes de connexions : le contrôle (requête puis réponse) et le flux
//! (après `Subscribe`, le daemon pousse seul `Output` jusqu'à `StreamEnd`).

use std::io::{ErrorKind, Read, Write};

use anyhow::{Result, bail};
use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};

use crate::daemon::{Command, ReloadReport, SessionInfo};

pub const VERSION: u32 = 3;
/// Début de toute connexion, avant le moindre message : sert à reconnaître un client
/// lazyshell et à comparer les versions, quel que soit le codage des messages.
const MAGIC: &[u8; 4] = b"LZSH";
const MAX_FRAME: u32 = 16 * 1024 * 1024;

#[derive(Debug, Serialize, Deserialize)]
pub enum Request {
    List,
    Command(Command),
    /// Taille du panneau d'affichage ; le PTY de la session la suit.
    Resize { session: String, rows: u16, cols: u16 },
    /// Transforme la connexion en flux de sortie de la session.
    Subscribe { session: String },
    /// Relit le fichier de config et l'applique.
    Reload,
    /// Arrête toutes les sessions puis le daemon.
    Shutdown,
}

#[derive(Debug, Serialize, Deserialize)]
pub enum Response {
    Infos(Vec<SessionInfo>),
    Reloaded(ReloadReport),
    Ok,
    Error(String),
    /// Réponse à `Subscribe` ; le premier `Output` qui suit est l'historique.
    Subscribed { run_id: u64 },
    Output(Vec<u8>),
    /// L'exécution est terminée, plus aucune sortie ne viendra.
    StreamEnd,
    /// Signe de vie sur un flux silencieux (détecte un client parti).
    Ping,
}

/// Échange de versions : 4 octets de signature puis la version (u32 big-endian). Chaque
/// côté envoie le sien ; ce format ne changera jamais, ce qui permet de signaler
/// proprement une incompatibilité au lieu d'échouer à décoder un message.
pub fn write_handshake(writer: &mut impl Write) -> Result<()> {
    writer.write_all(MAGIC)?;
    writer.write_all(&VERSION.to_be_bytes())?;
    writer.flush()?;
    Ok(())
}

/// Version annoncée par l'autre côté ; erreur si ce n'est pas un pair lazyshell.
pub fn read_handshake(reader: &mut impl Read) -> Result<u32> {
    let mut header = [0u8; 8];
    reader.read_exact(&mut header)?;
    if &header[..4] != MAGIC {
        bail!("l'autre côté ne parle pas le protocole lazyshell");
    }
    Ok(u32::from_be_bytes(header[4..].try_into().unwrap()))
}

pub fn write_message(writer: &mut impl Write, message: &impl Serialize) -> Result<()> {
    let payload = postcard::to_allocvec(message)?;
    writer.write_all(&(payload.len() as u32).to_be_bytes())?;
    writer.write_all(&payload)?;
    writer.flush()?;
    Ok(())
}

/// `None` si l'autre côté a fermé la connexion proprement entre deux messages.
pub fn read_message<T: DeserializeOwned>(reader: &mut impl Read) -> Result<Option<T>> {
    let mut len = [0u8; 4];
    match reader.read_exact(&mut len) {
        Ok(()) => {}
        Err(err) if err.kind() == ErrorKind::UnexpectedEof => return Ok(None),
        Err(err) => return Err(err.into()),
    }
    let len = u32::from_be_bytes(len);
    if len > MAX_FRAME {
        bail!("message trop gros ({len} octets)");
    }
    let mut payload = vec![0u8; len as usize];
    reader.read_exact(&mut payload)?;
    Ok(Some(postcard::from_bytes(&payload)?))
}
