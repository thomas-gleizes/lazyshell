//! Consommation CPU et mémoire d'une session, et ports TCP qu'elle écoute.
//!
//! Le PTY fait du shell le leader de sa session système : tous les processus lancés
//! par la commande (enfants, petits-enfants) partagent donc son identifiant de session.
//! La lecture dépend de l'OS : `/proc` sous Linux (`linux`), `libproc` sous macOS (`macos`).

use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};

#[cfg(target_os = "linux")]
mod linux;
#[cfg(target_os = "linux")]
use linux::{listening_ports, session_totals};

#[cfg(target_os = "macos")]
mod macos;
#[cfg(target_os = "macos")]
use macos::{listening_ports, session_totals};

/// Autres systèmes : pas de mesure (la TUI et `status` affichent alors `-`).
#[cfg(not(any(target_os = "linux", target_os = "macos")))]
fn session_totals(_session_id: u32) -> Option<Totals> {
    None
}

#[cfg(not(any(target_os = "linux", target_os = "macos")))]
fn listening_ports(_pids: &[u32]) -> Vec<u16> {
    Vec::new()
}

/// Intervalle minimal entre deux mesures : en dessous, la dernière valeur est réutilisée.
const SAMPLE_EVERY: Duration = Duration::from_secs(1);

/// Consommation instantanée d'une session.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Usage {
    /// Part d'un cœur utilisée, en pourcentage (peut dépasser 100 avec plusieurs threads).
    pub cpu_percent: f32,
    /// Mémoire résidente (RSS) cumulée, en octets.
    pub memory_bytes: u64,
    /// Ports TCP en écoute ouverts par les processus de la session, triés.
    pub ports: Vec<u16>,
}

/// Cumul des processus d'une session.
struct Totals {
    /// Temps CPU cumulé (utilisateur + système), en secondes.
    cpu_seconds: f64,
    /// Mémoire résidente, en octets.
    rss_bytes: u64,
    pids: Vec<u32>,
}

/// Mesures successives d'une exécution : le CPU est un écart entre deux relevés.
pub struct Sampler {
    session_id: u32,
    last_at: Instant,
    last_cpu_seconds: f64,
    latest: Option<Usage>,
}

impl Sampler {
    pub fn new(session_id: u32, started_at: Instant) -> Self {
        Sampler {
            session_id,
            last_at: started_at,
            last_cpu_seconds: 0.0,
            latest: None,
        }
    }

    /// Dernière mesure ; la rafraîchit si la précédente a plus d'une seconde.
    pub fn sample(&mut self) -> Option<Usage> {
        if self.session_id == 0 {
            return None;
        }
        let elapsed = self.last_at.elapsed();
        if self.latest.is_some() && elapsed < SAMPLE_EVERY {
            return self.latest.clone();
        }
        let totals = session_totals(self.session_id)?;
        let seconds = elapsed.as_secs_f64().max(0.001);
        // Un processus terminé entre deux relevés fait baisser le total : on borne à 0.
        let used = (totals.cpu_seconds - self.last_cpu_seconds).max(0.0);
        self.last_at = Instant::now();
        self.last_cpu_seconds = totals.cpu_seconds;
        let ports = listening_ports(&totals.pids);
        self.latest = Some(Usage {
            cpu_percent: (used / seconds * 100.0) as f32,
            memory_bytes: totals.rss_bytes,
            ports,
        });
        self.latest.clone()
    }
}
