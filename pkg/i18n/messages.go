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
	"action.focus_output": {
		"fr": "Aller au panneau de sortie",
		"en": "Go to the output panel",
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
	"action.new_named_session": {
		"fr": "Nouvelle session nommée",
		"en": "New named session",
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
	"action.jump_prev_prompt": {
		"fr": "Sauter au prompt précédent",
		"en": "Jump to the previous prompt",
	},
	"action.jump_next_prompt": {
		"fr": "Sauter au prompt suivant",
		"en": "Jump to the next prompt",
	},
	"action.copy_last_output": {
		"fr": "Copier la sortie de la dernière commande",
		"en": "Copy the last command's output",
	},
	"action.arm_watch": {
		"fr": "Armer/désarmer un motif de veille",
		"en": "Arm/disarm a watch pattern",
	},
	"action.set_group": {
		"fr": "Affecter la session à un groupe",
		"en": "Assign the session to a group",
	},
	"action.filter_group": {
		"fr": "Filtrer sur le groupe (à nouveau pour annuler)",
		"en": "Filter on the group (again to clear)",
	},
	"action.broadcast_group": {
		"fr": "Diffuser au groupe",
		"en": "Broadcast to the group",
	},
	"action.kill_group": {
		"fr": "Tuer le groupe",
		"en": "Kill the group",
	},
	"action.restart_group": {
		"fr": "Relancer le groupe",
		"en": "Restart the group",
	},
	"action.toggle_debug": {
		"fr": "Afficher ou masquer le panneau de debug",
		"en": "Show or hide the debug panel",
	},
	"action.next_tab": {
		"fr": "Onglet suivant du panneau de sortie",
		"en": "Next output panel tab",
	},
	"action.prev_tab": {
		"fr": "Onglet précédent du panneau de sortie",
		"en": "Previous output panel tab",
	},

	// The output panel's tab strip. Lower case throughout, like every other
	// label lazyshell draws (the " sessions " panel title, the footers).
	//
	// The *_short variants are the fallbacks used when the panel is too narrow
	// for the full strip, which gocui would otherwise truncate rather than
	// shorten — see pkg/gui/tabs.go's tabLabels. A translation should keep
	// them genuinely short: they exist for a panel of about 25 columns, and a
	// "short" form as long as the full one defeats the whole mechanism.
	"tab.terminal": {
		"fr": "Terminal",
		"en": "Terminal",
	},
	"tab.terminal_short": {
		"fr": "Term.",
		"en": "Term.",
	},
	"tab.resources": {
		"fr": "Ressources",
		"en": "Resources",
	},
	"tab.resources_short": {
		"fr": "Ress.",
		"en": "Res.",
	},
	"tab.environment": {
		"fr": "Environnement",
		"en": "Environment",
	},
	"tab.environment_short": {
		"fr": "Env",
		"en": "Env",
	},
	"tab.placeholder": {
		"fr": "  (à venir)",
		"en": "  (coming soon)",
	},

	"perf.shell": {
		"fr": "shell",
		"en": "shell",
	},
	"perf.foreground": {
		"fr": "avant-plan",
		"en": "foreground",
	},
	"perf.cpu": {
		"fr": "CPU",
		"en": "CPU",
	},
	"perf.rss": {
		"fr": "mémoire",
		"en": "memory",
	},
	"perf.threads": {
		"fr": "threads",
		"en": "threads",
	},
	"perf.disk": {
		"fr": "disque",
		"en": "disk",
	},
	"perf.disk_io": {
		"fr": "%s lus · %s écrits",
		"en": "%s read · %s written",
	},
	"perf.waiting": {
		"fr": "Mesure en cours…",
		"en": "Sampling…",
	},
	"perf.cpu_chart": {
		"fr": "CPU du %s — max %.1f %%",
		"en": "%s CPU — peak %.1f%%",
	},
	"perf.average": {
		"fr": "(moy. depuis le lancement)",
		"en": "(avg. since launch)",
	},
	"perf.unknown": {
		"fr": "inconnu",
		"en": "unknown",
	},
	"perf.unavailable_os": {
		"fr": "indisponible sur ce système",
		"en": "unavailable on this system",
	},
	"perf.unavailable": {
		"fr": "Mesure impossible : %s",
		"en": "Cannot measure: %s",
	},

	"env.count": {
		"fr": "%d variable(s) au lancement de la session",
		"en": "%d variable(s) at session launch",
	},
	"env.masked": {
		"fr": " — %d masquée(s)",
		"en": " — %d masked",
	},
	"env.empty": {
		"fr": "Aucune variable d'environnement",
		"en": "No environment variables",
	},

	"debug.title": {
		"fr": " debug ",
		"en": " debug ",
	},
	"debug.empty": {
		"fr": "aucun évènement pour l'instant",
		"en": "no events yet",
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
	// The Esc Esc variant, shown unless prefix_key is itself Escape — see
	// renderStatus.
	"status.passthrough_esc": {
		"fr": " -- INSERT --  (%s ou Esc Esc pour sortir) ",
		"en": " -- INSERT --  (%s or Esc Esc to exit) ",
	},
	"status.locked_hint": {
		"fr": " -- VERROUILLÉ --  (i/Entrée : reprendre la saisie) ",
		"en": " -- LOCKED --  (i/Enter: resume typing) ",
	},
	"status.search": {
		"fr": " /%s : %d/%d occurrence(s) ",
		"en": " /%s: %d/%d match(es) ",
	},
	"status.filter": {
		"fr": " filtre : %q — %d session(s) ",
		"en": " filter: %q — %d session(s) ",
	},
	"status.filter_group": {
		"fr": " groupe : %q — %d session(s) ",
		"en": " group: %q — %d session(s) ",
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
	"notify.command_failed": {
		"fr": "%s : commande en échec (%d)",
		"en": "%s: command failed (%d)",
	},
	"notify.watch_match": {
		"fr": "%s : motif « %s » repéré : %s",
		"en": "%s: pattern %q matched: %s",
	},

	"welcome.empty": {
		"fr": "Aucune session ouverte.",
		"en": "No session open.",
	},
	"welcome.filtered": {
		"fr": "Aucune session ne correspond au filtre.",
		"en": "No session matches the filter.",
	},

	"agents_panel.title": {
		"fr": "agents",
		"en": "agents",
	},
	"agents_panel.state_idle": {
		"fr": "inactif",
		"en": "idle",
	},
	"agents_panel.state_working": {
		"fr": "en cours",
		"en": "working",
	},
	"agents_panel.state_blocked": {
		"fr": "bloqué",
		"en": "blocked",
	},
	"agents_panel.state_done": {
		"fr": "terminé",
		"en": "done",
	},

	"sessions.empty": {
		"fr": "Aucune session — n pour en créer une",
		"en": "No sessions — press n to create one",
	},
	"sessions.group_ungrouped": {
		"fr": "sans groupe",
		"en": "ungrouped",
	},
	"sessions.kill_confirm": {
		"fr": "Tuer la session %q ? (y/n)",
		"en": "Kill session %q? (y/n)",
	},
	"sessions.delete_confirm": {
		"fr": "Supprimer définitivement la session %q ? Elle ne sera plus listée. (y/n)",
		"en": "Permanently delete session %q? It will no longer be listed. (y/n)",
	},
	"sessions.kill_group_confirm": {
		"fr": "Tuer les %[2]d session(s) du groupe %[1]q ? (y/n)",
		"en": "Kill the %[2]d session(s) of group %[1]q? (y/n)",
	},
	"sessions.quit_confirm": {
		"fr": "Agent(s) en cours d'exécution : %s. Quitter lazyshell quand même ? (y/n)",
		"en": "Agent(s) still running: %s. Quit lazyshell anyway? (y/n)",
	},
	"sessions.restart_group_none": {
		"fr": "groupe %s : aucune session terminée à relancer",
		"en": "group %s: no exited session to restart",
	},
	"sessions.restart_running": {
		"fr": "session %s : encore en cours d'exécution",
		"en": "session %s: still running",
	},

	"confirm.title": {
		"fr": " confirmer ",
		"en": " confirm ",
	},

	"busy.title": {
		"fr": " en cours ",
		"en": " working ",
	},
	"busy.kill": {
		"fr": "arrêt de la session %s...",
		"en": "stopping session %s...",
	},
	"busy.kill_group": {
		"fr": "arrêt du groupe %s...",
		"en": "stopping group %s...",
	},
	"busy.restart_group": {
		"fr": "relance du groupe %s...",
		"en": "restarting group %s...",
	},
	"busy.delete": {
		"fr": "suppression de la session %s...",
		"en": "deleting session %s...",
	},
	"busy.export": {
		"fr": "export vers %s...",
		"en": "exporting to %s...",
	},
	"busy.clipboard": {
		"fr": "copie vers le presse-papiers...",
		"en": "copying to the clipboard...",
	},

	"prompt.rename": {
		"fr": "renommer la session",
		"en": "rename the session",
	},
	"prompt.new_group": {
		"fr": "nouveau groupe",
		"en": "new group",
	},
	"prompt.watch_pattern": {
		"fr": "motif de veille (vide pour désarmer)",
		"en": "watch pattern (empty to disarm)",
	},
	"group_picker.title": {
		"fr": " groupe de la session ",
		"en": " session group ",
	},
	"group_picker.none": {
		"fr": "sans groupe",
		"en": "no group",
	},
	"group_picker.new": {
		"fr": "+ nouveau groupe…",
		"en": "+ new group…",
	},
	"prompt.new_named": {
		"fr": "nom de la session (vide = automatique)",
		"en": "session name (empty = automatic)",
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
	"copymode.no_last_command": {
		"fr": "aucune commande terminée à copier",
		"en": "no finished command to copy",
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
	"footer.new_named": {
		"fr": "nommer",
		"en": "name",
	},
	"footer.new_in_dir": {
		"fr": "dossier",
		"en": "folder",
	},
	"footer.set_group": {
		"fr": "groupe",
		"en": "group",
	},
	"footer.filter_group": {
		"fr": "filtrer groupe",
		"en": "filter group",
	},
	"footer.restart": {
		"fr": "relancer",
		"en": "restart",
	},
	"footer.zoom": {
		"fr": "zoom",
		"en": "zoom",
	},
	"footer.tab": {
		"fr": "onglet",
		"en": "tab",
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
