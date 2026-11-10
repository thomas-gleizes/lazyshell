//! Consommation CPU et mémoire d'une session, lue dans `/proc` (Linux).
//!
//! Le PTY fait du shell le leader de sa session système : tous les processus lancés
//! par la commande (enfants, petits-enfants) partagent donc son identifiant de session.

use std::collections::HashMap;
use std::fs;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};

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

/// Mesures successives d'une exécution : le CPU est un écart entre deux relevés.
pub struct Sampler {
    session_id: u32,
    last_at: Instant,
    last_ticks: u64,
    latest: Option<Usage>,
}

impl Sampler {
    pub fn new(session_id: u32, started_at: Instant) -> Self {
        Sampler {
            session_id,
            last_at: started_at,
            last_ticks: 0,
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
        let (ticks, rss_pages) = (totals.ticks, totals.rss_pages);
        let seconds = elapsed.as_secs_f32().max(0.001);
        // Un processus terminé entre deux relevés fait baisser le total : on borne à 0.
        let used = ticks.saturating_sub(self.last_ticks) as f32 / clock_ticks_per_second();
        self.last_at = Instant::now();
        self.last_ticks = ticks;
        let ports = listening_ports(&totals.pids);
        self.latest = Some(Usage {
            cpu_percent: used / seconds * 100.0,
            memory_bytes: rss_pages * page_size(),
            ports,
        });
        self.latest.clone()
    }
}

fn clock_ticks_per_second() -> f32 {
    (unsafe { libc::sysconf(libc::_SC_CLK_TCK) }).max(1) as f32
}

fn page_size() -> u64 {
    (unsafe { libc::sysconf(libc::_SC_PAGESIZE) }).max(1) as u64
}

/// Cumul des processus d'une session.
struct Totals {
    /// Temps CPU (utilisateur + système), en ticks d'horloge.
    ticks: u64,
    /// Mémoire résidente, en pages.
    rss_pages: u64,
    pids: Vec<u32>,
}

/// Cumule tous les processus de la session. `None` si `/proc` est illisible
/// (autre système que Linux).
fn session_totals(session_id: u32) -> Option<Totals> {
    let mut totals = Totals {
        ticks: 0,
        rss_pages: 0,
        pids: Vec::new(),
    };
    for entry in fs::read_dir("/proc").ok()?.flatten() {
        let name = entry.file_name();
        if !name
            .to_str()
            .is_some_and(|n| n.bytes().all(|b| b.is_ascii_digit()))
        {
            continue;
        }
        // Le processus peut disparaître entre la liste et la lecture : on l'ignore.
        let Ok(stat) = fs::read_to_string(entry.path().join("stat")) else {
            continue;
        };
        if let Some((sid, cpu, pages)) = parse_stat(&stat)
            && sid == session_id
        {
            totals.ticks += cpu;
            totals.rss_pages += pages;
            totals
                .pids
                .extend(name.to_str().and_then(|n| n.parse::<u32>().ok()));
        }
    }
    Some(totals)
}

/// Ports TCP en écoute dont la socket appartient à l'un des processus. On retrouve le
/// lien par l'inode : `/proc/net/tcp*` donne inode -> port, `/proc/<pid>/fd` donne les
/// inodes que chaque processus détient.
fn listening_ports(pids: &[u32]) -> Vec<u16> {
    let mut by_inode = HashMap::new();
    for table in ["/proc/net/tcp", "/proc/net/tcp6"] {
        if let Ok(content) = fs::read_to_string(table) {
            by_inode.extend(content.lines().skip(1).filter_map(parse_listener));
        }
    }
    if by_inode.is_empty() {
        return Vec::new();
    }
    let mut ports = Vec::new();
    for pid in pids {
        let Ok(fds) = fs::read_dir(format!("/proc/{pid}/fd")) else {
            continue;
        };
        for fd in fds.flatten() {
            let Ok(target) = fs::read_link(fd.path()) else {
                continue;
            };
            let inode = target
                .to_str()
                .and_then(|t| t.strip_prefix("socket:["))
                .and_then(|t| t.strip_suffix(']'));
            if let Some(port) = inode
                .and_then(|i| i.parse::<u64>().ok())
                .and_then(|i| by_inode.get(&i))
            {
                ports.push(*port);
            }
        }
    }
    ports.sort_unstable();
    ports.dedup();
    ports
}

/// Ligne de `/proc/net/tcp` -> (inode, port) si la socket est en écoute (état `0A`).
fn parse_listener(line: &str) -> Option<(u64, u16)> {
    let fields: Vec<&str> = line.split_whitespace().collect();
    if *fields.get(3)? != "0A" {
        return None;
    }
    let port = u16::from_str_radix(fields.get(1)?.rsplit(':').next()?, 16).ok()?;
    Some((fields.get(9)?.parse().ok()?, port))
}

/// Extrait (session, utime + stime, rss) d'une ligne de `/proc/<pid>/stat`. Le nom du
/// processus est entre parenthèses et peut contenir des espaces : on coupe après la dernière.
fn parse_stat(stat: &str) -> Option<(u32, u64, u64)> {
    let rest = &stat[stat.rfind(')')? + 1..];
    let fields: Vec<&str> = rest.split_whitespace().collect();
    let session = fields.get(3)?.parse().ok()?;
    let utime: u64 = fields.get(11)?.parse().ok()?;
    let stime: u64 = fields.get(12)?.parse().ok()?;
    let rss: i64 = fields.get(21)?.parse().ok()?;
    Some((session, utime + stime, rss.max(0) as u64))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_stat_handles_spaces_in_the_name() {
        let line = "1234 (my (odd) app) S 1 1234 1234 34816 1234 4194560 100 0 0 0 7 5 0 0 20 0 1 0 100 1000000 250 18446744073709551615";
        assert_eq!(parse_stat(line), Some((1234, 12, 250)));
    }

    #[test]
    fn current_process_has_memory() {
        let sid = unsafe { libc::getsid(0) } as u32;
        let totals = session_totals(sid).unwrap();
        assert!(totals.rss_pages > 0);
        assert!(totals.pids.contains(&std::process::id()));
    }

    #[test]
    fn parse_listener_keeps_only_listening_sockets() {
        let listen = "   0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 98765 1 0000 100 0 0 10 0";
        let connected = "   1: 0100007F:1F90 0100007F:D2A4 01 00000000:00000000 00:00000000 00000000  1000        0 11111 1 0000 100 0 0 10 0";
        assert_eq!(parse_listener(listen), Some((98765, 8080)));
        assert_eq!(parse_listener(connected), None);
    }

    #[test]
    fn finds_a_port_opened_by_this_process() {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        assert!(listening_ports(&[std::process::id()]).contains(&port));
    }
}
