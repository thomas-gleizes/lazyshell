//! Mesures lues avec `libproc` (macOS), qui n'a pas de `/proc`.
//!
//! macOS ne donne pas l'identifiant de session d'un autre processus. Or le PTY fait du shell
//! le leader de sa session : son pid *est* l'identifiant de session. On retrouve donc les
//! processus de la session en descendant l'arbre des parents depuis ce pid, et on y ajoute
//! ceux restés dans son groupe de processus (un enfant orphelin est rattaché à `launchd`,
//! mais garde son groupe).

use std::collections::{HashMap, HashSet};

use libproc::bsd_info::BSDInfo;
use libproc::file_info::{ListFDs, ProcFDType, pidfdinfo};
use libproc::net_info::{SocketFDInfo, SocketInfoKind, TcpSIState};
use libproc::proc_pid::{listpidinfo, pidinfo};
use libproc::processes::{ProcFilter, pids_by_type};
use libproc::task_info::TaskInfo;

use super::Totals;

/// Marge sur le nombre de descripteurs annoncé, qui peut bouger entre deux appels.
const FD_SLACK: usize = 16;

/// Nanosecondes par unité de temps Mach. Les temps CPU de `proc_taskinfo` sont dans cette
/// unité : elle vaut 1 sur Intel mais 125/3 sur Apple Silicon.
fn nanos_per_mach_tick() -> f64 {
    // `libc` marque ces symboles obsolètes au profit de `mach2`, mais ils restent corrects :
    // inutile d'ajouter une dépendance pour un seul appel.
    #[allow(deprecated)]
    let (status, info) = unsafe {
        let mut info: libc::mach_timebase_info = std::mem::zeroed();
        (libc::mach_timebase_info(&mut info), info)
    };
    if status != 0 || info.denom == 0 {
        return 1.0;
    }
    f64::from(info.numer) / f64::from(info.denom)
}

/// Processus de la session de `session_id` (le shell, ses descendants, son groupe).
fn session_members(session_id: u32) -> Option<Vec<u32>> {
    let mut children: HashMap<u32, Vec<u32>> = HashMap::new();
    let mut infos: HashMap<u32, BSDInfo> = HashMap::new();
    for pid in pids_by_type(ProcFilter::All).ok()? {
        // Le processus peut disparaître entre la liste et la lecture : on l'ignore.
        let Ok(info) = pidinfo::<BSDInfo>(pid as i32, 0) else {
            continue;
        };
        children.entry(info.pbi_ppid).or_default().push(pid);
        infos.insert(pid, info);
    }

    // Racines de la recherche : le shell et les processus restés dans son groupe.
    let mut members: HashSet<u32> = infos
        .iter()
        .filter(|(_, info)| info.pbi_pgid == session_id)
        .map(|(&pid, _)| pid)
        .collect();
    let mut stack: Vec<u32> = members.iter().copied().collect();
    stack.push(session_id);
    let mut visited = HashSet::new();
    while let Some(pid) = stack.pop() {
        if !visited.insert(pid) {
            continue;
        }
        if infos.contains_key(&pid) {
            members.insert(pid);
        }
        stack.extend(children.get(&pid).into_iter().flatten().copied());
    }
    Some(members.into_iter().collect())
}

/// Cumule tous les processus de la session. `None` si la liste des processus est illisible.
pub fn session_totals(session_id: u32) -> Option<Totals> {
    let members = session_members(session_id)?;
    let mut mach_ticks: u64 = 0;
    let mut rss_bytes: u64 = 0;
    let mut pids = Vec::new();
    for pid in members {
        let Ok(task) = pidinfo::<TaskInfo>(pid as i32, 0) else {
            continue;
        };
        mach_ticks += task.pti_total_user + task.pti_total_system;
        rss_bytes += task.pti_resident_size;
        pids.push(pid);
    }
    Some(Totals {
        cpu_seconds: mach_ticks as f64 * nanos_per_mach_tick() / 1e9,
        rss_bytes,
        pids,
    })
}

/// Ports TCP en écoute ouverts par l'un des processus, triés.
pub fn listening_ports(pids: &[u32]) -> Vec<u16> {
    let mut ports = Vec::new();
    for &pid in pids {
        let pid = pid as i32;
        let Ok(info) = pidinfo::<BSDInfo>(pid, 0) else {
            continue;
        };
        let Ok(fds) = listpidinfo::<ListFDs>(pid, info.pbi_nfiles as usize + FD_SLACK) else {
            continue;
        };
        for fd in fds {
            if !matches!(ProcFDType::from(fd.proc_fdtype), ProcFDType::Socket) {
                continue;
            }
            let Ok(socket) = pidfdinfo::<SocketFDInfo>(pid, fd.proc_fd) else {
                continue;
            };
            if socket.psi.soi_kind != SocketInfoKind::Tcp as i32 {
                continue;
            }
            // SAFETY: `soi_kind` vaut `Tcp`, donc c'est la variante `pri_tcp` qui est active.
            let tcp = unsafe { socket.psi.soi_proto.pri_tcp };
            if tcp.tcpsi_state == TcpSIState::Listen as i32 {
                // Le port est stocké en ordre réseau (big-endian) dans un entier de 32 bits.
                ports.push(u16::from_be(tcp.tcpsi_ini.insi_lport as u16));
            }
        }
    }
    ports.sort_unstable();
    ports.dedup();
    ports
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn current_process_has_memory() {
        let pid = std::process::id();
        let totals = session_totals(pid).unwrap();
        assert!(totals.rss_bytes > 0);
        assert!(totals.pids.contains(&pid));
    }

    #[test]
    fn finds_a_port_opened_by_this_process() {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        assert!(listening_ports(&[std::process::id()]).contains(&port));
    }
}
