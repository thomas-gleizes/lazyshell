use std::collections::HashMap;
use std::path::Path;

use anyhow::{Context, Result, bail};

/// Lit un fichier d'env au format `.env`. Un fichier absent est ignoré (résultat vide).
pub fn load(path: &Path) -> Result<HashMap<String, String>> {
    let text = match std::fs::read_to_string(path) {
        Ok(text) => text,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(HashMap::new()),
        Err(err) => return Err(err).with_context(|| format!("lecture de {}", path.display())),
    };
    parse(&text).with_context(|| format!("fichier d'env invalide : {}", path.display()))
}

/// Syntaxe : `KEY=value`, préfixe `export` accepté, lignes vides et `#` ignorées,
/// valeurs entre `"` (échappements `\n \t \" \\`) ou `'` (littérales). Pas d'interpolation.
pub fn parse(text: &str) -> Result<HashMap<String, String>> {
    let mut vars = HashMap::new();
    for (index, raw) in text.lines().enumerate() {
        let line = raw.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let line = line.strip_prefix("export ").map(str::trim_start).unwrap_or(line);
        let Some((key, value)) = line.split_once('=') else {
            bail!("ligne {} : « KEY=valeur » attendu", index + 1);
        };
        let key = key.trim();
        if key.is_empty() || !key.chars().all(|c| c.is_ascii_alphanumeric() || c == '_') {
            bail!("ligne {} : nom de variable invalide « {key} »", index + 1);
        }
        vars.insert(key.to_string(), parse_value(value.trim(), index + 1)?);
    }
    Ok(vars)
}

fn parse_value(value: &str, line: usize) -> Result<String> {
    if let Some(rest) = value.strip_prefix('"') {
        let mut out = String::new();
        let mut chars = rest.chars();
        while let Some(c) = chars.next() {
            match c {
                '"' => return Ok(out),
                '\\' => match chars.next() {
                    Some('n') => out.push('\n'),
                    Some('t') => out.push('\t'),
                    Some(other) => out.push(other),
                    None => break,
                },
                c => out.push(c),
            }
        }
        bail!("ligne {line} : guillemet fermant manquant");
    }
    if let Some(rest) = value.strip_prefix('\'') {
        return match rest.find('\'') {
            Some(end) => Ok(rest[..end].to_string()),
            None => bail!("ligne {line} : guillemet fermant manquant"),
        };
    }
    // Valeur nue : un ` #` démarre un commentaire.
    let end = value.find(" #").unwrap_or(value.len());
    Ok(value[..end].trim_end().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_common_forms() {
        let vars = parse(
            "# commentaire\n\nA=1\nexport B = deux # note\nC=\"x\\ny # pas un commentaire\"\nD='a\\nb'\nE=\n",
        )
        .unwrap();
        assert_eq!(vars["A"], "1");
        assert_eq!(vars["B"], "deux");
        assert_eq!(vars["C"], "x\ny # pas un commentaire");
        assert_eq!(vars["D"], "a\\nb");
        assert_eq!(vars["E"], "");
    }

    #[test]
    fn rejects_garbage() {
        assert!(parse("pas de egal").is_err());
        assert!(parse("A B=1").is_err());
        assert!(parse("A=\"ouvert").is_err());
    }

    #[test]
    fn missing_file_is_ignored() {
        assert!(load(Path::new("/nonexistent/.env")).unwrap().is_empty());
    }
}
