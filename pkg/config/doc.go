package config

// site/assets/config-schema.json is the site's config editor's data — see
// cmd/gen-config-schema and ROADMAP.md's "Éditeur de configuration graphique
// sur le site statique". go generate runs a directive's command with this
// file's directory (pkg/config) as its working directory, but the generator
// itself reads README.md/docs/README.fr.md and writes site/assets/ as paths
// relative to the repository root — hence the "cd ../.." rather than a plain
// "go run ../../cmd/gen-config-schema". Unix-only, matching this project's
// no-Windows-support stance.
//go:generate sh -c "cd ../.. && go run ./cmd/gen-config-schema"
