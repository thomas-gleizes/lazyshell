//! Textes affichés à l'utilisateur (TUI et sorties de la CLI), en français et en anglais.
//! Les journaux du daemon et les messages d'erreur internes restent en français.

use std::sync::OnceLock;

use serde::Deserialize;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Deserialize)]
pub enum Lang {
    #[default]
    #[serde(rename = "fr")]
    Fr,
    #[serde(rename = "en")]
    En,
}

static LANG: OnceLock<Lang> = OnceLock::new();

/// À appeler une fois au démarrage ; sans appel, le français est utilisé.
pub fn init(lang: Lang) {
    let _ = LANG.set(lang);
}

fn pick(fr: String, en: String) -> String {
    match LANG.get().copied().unwrap_or_default() {
        Lang::Fr => fr,
        Lang::En => en,
    }
}

fn pick_static(fr: &'static str, en: &'static str) -> &'static str {
    match LANG.get().copied().unwrap_or_default() {
        Lang::Fr => fr,
        Lang::En => en,
    }
}

// --- États des sessions ---

pub fn stopped() -> &'static str {
    pick_static("arrêtée", "stopped")
}

pub fn stopping() -> &'static str {
    pick_static("arrêt…", "stopping…")
}

pub fn finished() -> &'static str {
    pick_static("terminée", "finished")
}

pub fn failed(code: i32) -> String {
    pick(format!("échec ({code})"), format!("failed ({code})"))
}

pub fn running(secs: u64) -> String {
    pick(format!("en cours ({secs}s)"), format!("running ({secs}s)"))
}

// --- CLI ---

pub fn no_session_running() -> &'static str {
    pick_static("aucune session en cours", "no session running")
}

pub fn ps_header(config_width: usize) -> String {
    pick(
        format!("{:<config_width$}  {:<14} {:<20} ÉTAT", "CONFIG", "GROUPE", "SESSION"),
        format!("{:<config_width$}  {:<14} {:<20} STATE", "CONFIG", "GROUP", "SESSION"),
    )
}

pub fn session_stopped(config: &str, name: &str) -> String {
    pick(format!("{config} : {name} arrêtée"), format!("{config}: {name} stopped"))
}

// --- TUI ---

pub fn stopping_all() -> &'static str {
    pick_static("Arrêt de toutes les sessions…", "Stopping all sessions…")
}

pub fn config_reloaded(summary: &str) -> String {
    pick(format!("Config rechargée : {summary}"), format!("Config reloaded: {summary}"))
}

pub fn reload_refused(err: &dyn std::fmt::Display) -> String {
    pick(format!("Rechargement refusé : {err}"), format!("Reload refused: {err}"))
}

pub fn no_search() -> &'static str {
    pick_static("Aucune recherche en cours (/ pour chercher)", "No search in progress (/ to search)")
}

pub fn no_match(query: &str) -> String {
    pick(format!("Aucune occurrence de « {query} »"), format!("No match for \"{query}\""))
}

pub fn no_other_match() -> &'static str {
    pick_static("Pas d'autre occurrence dans ce sens", "No other match in this direction")
}

pub fn search_prompt(input: &str) -> String {
    pick(
        format!("/{input}█   Entrée valider  Échap annuler"),
        format!("/{input}█   Enter confirm  Esc cancel"),
    )
}

pub fn footer_hints() -> &'static str {
    pick_static(
        "j/k naviguer  s lancer  x arrêter  r relancer  / chercher  L recharger  ? aide  q quitter",
        "j/k navigate  s start  x stop  r restart  / search  L reload  ? help  q quit",
    )
}

pub fn sessions_title() -> &'static str {
    pick_static(" Sessions ", " Sessions ")
}

pub fn help_title() -> &'static str {
    pick_static(" Aide ", " Help ")
}

pub fn session_stopped_hint() -> &'static str {
    pick_static("Session arrêtée — s pour la lancer", "Session stopped — press s to start it")
}

const HELP_FR: &str = "\
j/k ↑/↓    session suivante / précédente
s          lancer
x          arrêter (SIGTERM, puis SIGKILL après 10 s)
r          relancer
S / X / R  lancer / arrêter / relancer tout le groupe
PgUp/PgDn  défiler la sortie
molette    défiler la sortie
g / G      début de l'historique / retour au direct
/          chercher dans la sortie (Entrée : occurrence la plus récente)
n / N      occurrence plus ancienne / plus récente   Échap : effacer
L          recharger la config (sessions en cours inchangées)
~          (dans la liste) config modifiée : relancer pour l'appliquer
q          quitter la TUI (les sessions continuent)
Q          arrêter toutes les sessions et le daemon

Touche quelconque pour fermer.";

const HELP_EN: &str = "\
j/k ↑/↓    next / previous session
s          start
x          stop (SIGTERM, then SIGKILL after 10 s)
r          restart
S / X / R  start / stop / restart the whole group
PgUp/PgDn  scroll the output
wheel      scroll the output
g / G      top of history / back to live
/          search the output (Enter: most recent match)
n / N      older / newer match   Esc: clear
L          reload the config (running sessions unchanged)
~          (in the list) config changed: restart to apply it
q          quit the TUI (sessions keep running)
Q          stop all sessions and the daemon

Press any key to close.";

pub fn help_text() -> &'static str {
    pick_static(HELP_FR, HELP_EN)
}

// --- Rapport de rechargement ---

pub fn reload_added() -> &'static str {
    pick_static("ajoutées", "added")
}

pub fn reload_removed() -> &'static str {
    pick_static("retirées", "removed")
}

pub fn reload_changed() -> &'static str {
    pick_static("modifiées (effet au prochain démarrage)", "changed (applied on next start)")
}

pub fn reload_no_change() -> &'static str {
    pick_static("aucun changement", "no changes")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn help_texts_have_the_same_height() {
        // La hauteur de la fenêtre d'aide est fixe : les deux langues doivent tenir dedans.
        assert_eq!(HELP_FR.lines().count(), HELP_EN.lines().count());
    }
}
