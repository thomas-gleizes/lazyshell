package i18n

// messages is every user-facing string the interactive TUI shows, keyed by a
// stable id and then by language. TestMessagesAreComplete fails the build if
// a key here is missing either "fr" or "en" — the two Config.Languages
// accepts.
//
// CLI output (pkg/app) and config validation errors (pkg/config) are not
// covered here: both can run before a config file — and so a Language — has
// been loaded at all. They stay French, as they already were.
var messages = map[string]map[string]string{
	"action.quit": {
		"fr": "Quitter lazyshell",
		"en": "Quit lazyshell",
	},
	"action.cycle_focus": {
		"fr": "Changer de panneau actif",
		"en": "Switch focused panel",
	},
	"action.help": {
		"fr": "Afficher l'aide",
		"en": "Show help",
	},
	"action.select_next": {
		"fr": "Session suivante",
		"en": "Next session",
	},
	"action.select_prev": {
		"fr": "Session précédente",
		"en": "Previous session",
	},
	"action.new_session": {
		"fr": "Nouvelle session",
		"en": "New session",
	},
	"action.kill_session": {
		"fr": "Tuer la session sélectionnée",
		"en": "Kill the selected session",
	},
	"action.delete_session": {
		"fr": "Supprimer définitivement la session sélectionnée",
		"en": "Permanently delete the selected session",
	},
	"action.rename_session": {
		"fr": "Renommer la session sélectionnée",
		"en": "Rename the selected session",
	},
	"action.duplicate_session": {
		"fr": "Dupliquer la session sélectionnée",
		"en": "Duplicate the selected session",
	},
	"action.new_session_in_dir": {
		"fr": "Nouvelle session dans un dossier choisi",
		"en": "New session in a chosen directory",
	},
	"action.restart_session": {
		"fr": "Relancer la session sélectionnée",
		"en": "Restart the selected session",
	},
	"action.zoom": {
		"fr": "Zoomer/dézoomer le panneau de sortie",
		"en": "Zoom/unzoom the output panel",
	},
	"action.jump": {
		"fr": "Aller directement à la session %d",
		"en": "Jump directly to session %d",
	},
	"action.filter_sessions": {
		"fr": "Filtrer la liste des sessions",
		"en": "Filter the sessions list",
	},
	"action.clear_filter": {
		"fr": "Effacer le filtre de la liste des sessions",
		"en": "Clear the sessions list filter",
	},
	"action.export_session": {
		"fr": "Exporter le scrollback de la session sélectionnée",
		"en": "Export the selected session's scrollback",
	},
	"action.toggle_broadcast": {
		"fr": "Marquer/démarquer la session pour la diffusion",
		"en": "Mark/unmark the session for broadcast",
	},
	"action.jump_next_blocked": {
		"fr": "Sauter à la prochaine session bloquée",
		"en": "Jump to the next blocked session",
	},

	"help.title": {
		"fr": " aide ",
		"en": " help ",
	},
	"help.header": {
		"fr": "Aide — j/k pour naviguer, Entrée pour exécuter, Échap pour fermer",
		"en": "Help — j/k to navigate, Enter to run, Esc to close",
	},
	"help.actions_available": {
		"fr": "Actions disponibles",
		"en": "Available actions",
	},
	"help.actions_unavailable": {
		"fr": "Actions indisponibles (aucune session sélectionnée ou concernée)",
		"en": "Unavailable actions (no relevant session selected)",
	},
	"help.mouse_header": {
		"fr": "Souris",
		"en": "Mouse",
	},
	"help.mouse_click": {
		"fr": "clic : sélectionner une session (sans passer en INSERT)",
		"en": "click: select a session (without entering INSERT)",
	},
	"help.mouse_double_click": {
		"fr": "double-clic : sélectionner et prendre la main sur le shell",
		"en": "double click: select and hand the keyboard to the shell",
	},
	"help.mouse_wheel": {
		"fr": "molette : faire défiler le contenu du panneau",
		"en": "wheel: scroll the panel's content",
	},
	"help.mouse_drag": {
		"fr": "glisser : sélectionner des lignes, puis y pour copier",
		"en": "drag: select lines, then y to copy",
	},

	"status.hint": {
		"fr": " n: nouvelle session   x/d: tuer   j/k: naviguer   Tab: changer de focus   ?: aide   q: quitter ",
		"en": " n: new session   x/d: kill   j/k: navigate   Tab: switch panel   ?: help   q: quit ",
	},
	"status.passthrough": {
		"fr": " -- INSERT --  (%s pour sortir) ",
		"en": " -- INSERT --  (%s to exit) ",
	},
	"status.search": {
		"fr": " /%s : %d/%d occurrence(s) ",
		"en": " /%s: %d/%d match(es) ",
	},
	"status.filter": {
		"fr": " filtre : %q — %d session(s) ",
		"en": " filter: %q — %d session(s) ",
	},
	"status.copymode": {
		"fr": " -- SÉLECTION -- %d ligne(s) — y : copier, Esc : annuler ",
		"en": " -- SELECT -- %d line(s) — y: copy, Esc: cancel ",
	},
	"status.broadcast": {
		"fr": " ⚠ DIFFUSION → %d sessions ",
		"en": " ⚠ BROADCAST → %d sessions ",
	},

	"notify.blocked": {
		"fr": "%s attend une réponse",
		"en": "%s is waiting for a response",
	},
	"notify.done": {
		"fr": "%s a terminé",
		"en": "%s is done",
	},

	"sessions.empty": {
		"fr": "Aucune session — n pour en créer une",
		"en": "No sessions — press n to create one",
	},
	"sessions.kill_confirm": {
		"fr": "Tuer la session %q ? (y/n)",
		"en": "Kill session %q? (y/n)",
	},
	"sessions.delete_confirm": {
		"fr": "Supprimer définitivement la session %q ? Elle ne sera plus listée. (y/n)",
		"en": "Permanently delete session %q? It will no longer be listed. (y/n)",
	},
	"sessions.restart_running": {
		"fr": "session %s : encore en cours d'exécution",
		"en": "session %s: still running",
	},

	"confirm.title": {
		"fr": " confirmer ",
		"en": " confirm ",
	},

	"prompt.rename": {
		"fr": "renommer la session",
		"en": "rename the session",
	},
	"prompt.new_in_dir": {
		"fr": "nouvelle session dans...",
		"en": "new session in...",
	},
	"prompt.export": {
		"fr": "exporter vers...",
		"en": "export to...",
	},

	"search.title": {
		"fr": "rechercher",
		"en": "search",
	},
	"search.no_matches": {
		"fr": "aucune occurrence pour %q",
		"en": "no matches for %q",
	},

	"filter.title": {
		"fr": "filtrer les sessions",
		"en": "filter sessions",
	},

	"copymode.copy_failed": {
		"fr": "copie impossible : %s",
		"en": "copy failed: %s",
	},

	"export.success": {
		"fr": "scrollback exporté vers %s",
		"en": "scrollback exported to %s",
	},
	"export.failed": {
		"fr": "export impossible : %s",
		"en": "export failed: %s",
	},

	"input.session_exited": {
		"fr": "session %s terminée (code %d)",
		"en": "session %s exited (code %d)",
	},

	"footer.new": {
		"fr": "nouvelle",
		"en": "new",
	},
	"footer.kill": {
		"fr": "tuer",
		"en": "kill",
	},
	"footer.delete": {
		"fr": "supprimer",
		"en": "delete",
	},
	"footer.navigate": {
		"fr": "naviguer",
		"en": "navigate",
	},
	"footer.rename": {
		"fr": "renommer",
		"en": "rename",
	},
	"footer.duplicate": {
		"fr": "dupliquer",
		"en": "duplicate",
	},
	"footer.new_in_dir": {
		"fr": "dossier",
		"en": "folder",
	},
	"footer.restart": {
		"fr": "relancer",
		"en": "restart",
	},
	"footer.zoom": {
		"fr": "zoom",
		"en": "zoom",
	},
	"footer.filter": {
		"fr": "filtrer",
		"en": "filter",
	},
	"footer.export": {
		"fr": "exporter",
		"en": "export",
	},
	"footer.broadcast": {
		"fr": "diffusion",
		"en": "broadcast",
	},
	"footer.jump_next_blocked": {
		"fr": "session bloquée suivante",
		"en": "next blocked session",
	},
	"footer.exit_passthrough": {
		"fr": "sortir",
		"en": "exit",
	},
	"footer.type": {
		"fr": "saisir",
		"en": "type",
	},
	"footer.scroll": {
		"fr": "défiler",
		"en": "scroll",
	},
	"footer.half_page": {
		"fr": "demi-page",
		"en": "half page",
	},
	"footer.search": {
		"fr": "chercher",
		"en": "search",
	},
	"footer.search_next": {
		"fr": "occurrence suiv./préc.",
		"en": "next/prev match",
	},
	"footer.search_clear": {
		"fr": "quitter la recherche",
		"en": "exit search",
	},
	"footer.copymode_enter": {
		"fr": "sélectionner",
		"en": "select",
	},
	"footer.copymode_move": {
		"fr": "étendre la sélection",
		"en": "extend selection",
	},
	"footer.copymode_yank": {
		"fr": "copier",
		"en": "copy",
	},
	"footer.copymode_cancel": {
		"fr": "annuler",
		"en": "cancel",
	},
}
