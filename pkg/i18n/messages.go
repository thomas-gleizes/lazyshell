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

	"help.title": {
		"fr": " aide ",
		"en": " help ",
	},
	"help.header": {
		"fr": "Aide — une touche quelconque pour fermer",
		"en": "Help — any key to close",
	},

	"status.hint": {
		"fr": " n: nouvelle session   x/d: tuer   j/k: naviguer   Tab: changer de focus   ?: aide   q: quitter ",
		"en": " n: new session   x/d: kill   j/k: navigate   Tab: switch panel   ?: help   q: quit ",
	},
	"status.passthrough": {
		"fr": " -- INSERT --  (%s pour sortir) ",
		"en": " -- INSERT --  (%s to exit) ",
	},

	"sessions.empty": {
		"fr": "Aucune session — n pour en créer une",
		"en": "No sessions — press n to create one",
	},
	"sessions.kill_confirm": {
		"fr": "Tuer la session %q ? (y/n)",
		"en": "Kill session %q? (y/n)",
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
}
