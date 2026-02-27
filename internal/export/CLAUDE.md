# export

Responsabilite: Export JSONL de proof trees avec anonymisation des auteurs et metadonnees structurees, incluant les CorrectedGarbageSets pour la demolition de fausses affirmations.
Depend de: `internal/db`
Dependants: `internal/api`
Point d'entree: `export.go`
Types cles: `TreeExport`, `ExportNode`, `ExportSource`, `ExportMetadata`, `CorrectedGarbageSet`, `CorrectedGSMetadata`
Invariants: Les author_id sont anonymises par hash SHA256 + salt aleatoire. L'export est auto-contenu (arbre complet + metadata).
NE PAS: Inclure des author_id non anonymises dans les exports. Modifier le format sans incrementer export_version.
