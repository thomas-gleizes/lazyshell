//! Cœur de la supervision : possède les sessions, leurs PTY et leurs sorties.
//! L'interface (TUI) ne passe que par [`Manager`] ; ce module sera déplacé
//! derrière une socket dans un processus daemon aux jalons suivants.

mod manager;
mod server;
mod session;

pub use server::run as serve;
pub use session::Subscription;
pub use manager::{Command, Manager, ReloadReport, SessionInfo, State, Target};
