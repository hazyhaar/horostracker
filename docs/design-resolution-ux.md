# Design : UX de la Resolution et Investigation Collaborative

> Reflexion UX, logique et semantique — pas de code.
> Date : 2026-02-13

---

## 1. Diagnostic

L'arbre actuel pousse dans toutes les directions. Les LLM ajoutent du volume sans
discrimination. La Resolution tente de tout compresser, produisant une synthese
exhaustive mais illisible. Le bruit n'est pas dans les contributions individuelles
— c'est l'absence de chemin structure entre la question ouverte et sa resolution.

---

## 2. Cinq chemins vers la clarte

### 2.1 Decomposition en assertions atomiques

Une question complexe genere un arbre plat avec des dizaines de branches au premier
niveau. Le chemin : avant toute contribution, la question est decomposee en
**assertions atomiques** — des affirmations verifiables individuellement. Chaque
assertion devient un micro-arbre autonome avec son propre cycle (sourcer, contester,
trancher). La Resolution finale n'est plus une synthese du bruit mais une
**recomposition d'assertions resolues**.

Le passage de la question floue aux assertions atomiques peut etre fait par un LLM
a l'entree, puis valide/amende par les usagers.

### 2.2 Phases explicites (diverger, puis converger)

Chaque question (ou assertion atomique) traverse des phases sequentielles, chaque
phase n'autorisant que certains types de noeuds :

| Phase | Noeuds autorises | Objectif |
|-------|------------------|----------|
| **Collecte** | reponses, sources | Accumuler du materiau brut |
| **Confrontation** | objections, precisions | Mettre a l'epreuve |
| **Elagage** | votes collectifs | Replier les branches non soutenues |
| **Distillation** | synthese, resolution | Ne porte que sur les branches survivantes |

> **Nuance importante** : les arbres ne sont jamais fermes. On peut toujours ajouter
> une source. Les phases sont des modes de priorite, pas des verrous. Ce qui change,
> c'est le moment ou la Resolution s'execute — apres que le signal est isole.

### 2.3 Entonnoir de pertinence

Chaque contribution recoit un **score de pertinence** (pas de qualite — de
pertinence par rapport a l'assertion). La Resolution ne prend en entree que les
contributions au-dessus d'un seuil. Le reste existe pour le forensic.

Role des LLM : au lieu de produire du contenu, certains LLM sont assignes au
**triage** — scorer la pertinence, detecter les doublons semantiques, identifier
les tangentes. Le bruit LLM devient filtre LLM.

### 2.4 Structure dialectique forcee

Forcer une structure **these / antithese / sources pour / sources contre** pour
chaque assertion. Chaque contribution doit se positionner explicitement. Elimine
les contributions "flottantes". Rend la Resolution triviale a structurer.

### 2.5 Source-first

Inversion du flux : le squelette de l'arbre est constitue de **sources**, les
interpretations viennent en second. Un LLM qui ne peut pas produire une source
verifiable ne rentre pas dans l'arbre principal.

---

## 3. La dark team : revue critique systematique

### 3.1 Le probleme

Chaque LLM est entraine a etre d'accord avec elegance. Meme quand on lui demande
d'objecter, il objecte poliment et cherche le compromis. RLHF minimise la friction.
Lancer un flow multi-modele produit cinq variations du meme accord deguise en debat.

### 3.2 La contradiction comme workflow, pas comme prompt

La dark team n'est pas un prompt "objecte avec des sources". C'est un **workflow
continu** dont l'objectif unique est de demolir l'assertion. Si la demolition
echoue, l'assertion en sort renforcee par l'echec documente de toutes les
tentatives.

Une assertion sans objection est "non contestee". Une assertion qui a survecu
a N tentatives de demolition a un **indice de resistance**. Donnee radicalement
differente.

### 3.3 Les cinq angles d'attaque

Chaque assertion passe par cinq filtres de deconstruction :

| Angle | Cible | Question |
|-------|-------|----------|
| **Logique** | Structure argumentative | Premisses implicites ? Sauts logiques ? Correlation/causalite ? |
| **Sources** | Pieces justificatives | Source primaire/secondaire ? Repliquee ? Conflits d'interets ? |
| **Contre-exemple** | Universalite | Quel cas casse l'assertion ? |
| **Reformulation** | Precision semantique | La reformulation charitable change-t-elle la conclusion ? |
| **Contexte manquant** | Completude | Cadre temporel/geographique/demographique implicite ? |

### 3.4 Profil de vulnerabilite

Le resultat n'est pas "vrai/faux". C'est une cartographie :

| Dimension | Spectre |
|-----------|---------|
| Resistance logique | solide / fragile |
| Ancrage empirique | source primaire / interpretation / non source |
| Robustesse au contre-exemple | universelle / contextuelle / anecdotique |
| Precision semantique | precise / ambigue / trompeuse par omission |
| Completude | autosuffisante / dependante de contexte implicite |

### 3.5 L'equipage d'une question

| Role | Fonction | Quand |
|------|----------|-------|
| **Architecte** | Decompose en assertions atomiques | En entree, une fois |
| **Sourcier** | Ancre chaque assertion dans le reel | En continu |
| **Demolisseur** | Cinq angles d'attaque systematiques | En continu |
| **Cartographe** | Maintient le profil de vulnerabilite | En continu |
| **Distillateur** | Produit la Resolution sur le materiau elague | En sortie, une fois |

---

## 4. Trois zones

### 4.1 Architecture

| Zone | Visibilite | Contenu |
|------|------------|---------|
| **Publique** | Tout le monde | Assertions, sources, profils de vulnerabilite, resolutions |
| **Operateurs** | Operateurs valides | Workflows de deconstruction, discussion des recettes, tests |
| **Providers** | Providers API | Metriques de performance, datasets anonymises |

### 4.2 Flux

- **Zone publique** : une assertion est posee → decomposee → les sources
  s'accumulent → le profil de vulnerabilite apparait → la resolution se construit.
- **Zone operateurs** : les workflows sont concus, discutes, versionnes, testes
  sur des assertions existantes en espace clone. Les resultats remontent en zone
  publique, mais la mecanique reste opaque.
- **Zone provider** : le compute. Les providers voient les metriques de leurs
  modeles mais pas le contenu des workflows.

### 4.3 Transparence du resultat, opacite de la methode

L'arbre public est tracable : chaque assertion a son profil, chaque source est
verifiable. L'usager voit **quoi** a ete attaque et ce qui a tenu. Il ne voit pas
**comment** l'attaque a ete construite.

Modele du pentest : les outils sont open source, les rapports specifiques et les
strategies combinees restent confidentiels pendant l'engagement.

---

## 5. "What's true today" — verite temporelle

### 5.1 Pas de chose jugee

Les arbres ne sont jamais fermes. On peut toujours ajouter une source. C'est
indispensable pour les audits tech (conformite, securite) et pertinent dans tous
les domaines. Le concept n'est pas "what's universal truth" mais
**"what's true today"**.

### 5.2 Resolutions comme snapshots

La Resolution n'est pas un jugement definitif — c'est un **instantane date**.
Chaque Resolution est immutable, reference les sources prises en compte. La
suivante reference les sources nouvelles et les changements. On obtient un
**changelog de la verite**.

### 5.3 Etats d'une assertion

| Etat | Signification |
|------|---------------|
| **Non contestee** | Aucune source adverse, aucune revue critique. Valeur probante faible. |
| **Resistante** | A survecu a N cycles de deconstruction a la date X. |
| **Fragilisee** | Une source recente a affaibli le profil. |
| **Tombee** | Une source a renverse l'assertion. Moment et source documentes. |
| **Ressuscitee** | Une assertion tombee remise debout par une source ulterieure. |

Le parcours complet est la donnee la plus precieuse : l'histoire de l'assertion.

---

## 6. Vocabulaire documentaire

Vocabulaire neutre pour avancer sans declencher de reflexes corporatistes.

| Terme juridique | Terme documentaire | Dans horostracker |
|-----------------|--------------------|-------------------|
| Claim / These | **Assertion** | Noeud racine ou assertion atomique |
| Piece / Exhibit | **Source** | Noeud de type piece : URL, document, hash |
| Objection | **Contradiction** | Noeud qui conteste avec justification |
| Partie adverse / Dark team | **Revue critique** | Workflow de deconstruction |
| Contradictoire | **Confrontation** | Mecanisme d'opposition des sources |
| Recevabilite | **Pertinence** | Filtre d'entree |
| Charge de la preuve | **Ancrage** | Obligation de rattacher a une source |
| Mise en etat | **Instruction** | Phase de preparation |
| Delibere | **Resolution** | Deja le bon mot — snapshot date |
| Jugement / Verdict | **Etat courant** | "What's true today" |
| Autorite de la chose jugee | **Indice de resistance** | Cycles de revue critique survecus |
| Appel | **Reouverture** | Ajout d'une source nouvelle |
| Motivation | **Profil de vulnerabilite** | Cartographie forces/faiblesses |
| Conclusions | **Synthese** | Export structure |
| Dossier | **Arbre** | Deja le bon mot |
| Mandat | **Invitation** | Acces nominatif avec perimetre |
| Revocation | **Revocation** | Retrait d'acces, horodate, immutable |
| Communication de pieces | **Partage** | Export partiel vers un tiers |
| Role (registre) | **Registre** | Identifiant unique d'un arbre |
| Juridiction | **Projection** | Couche de labels specifique a un contexte |
| Commission rogatoire | **Synchronisation** | Echange de noeuds entre instances |
| Chronologie des faits | **Timeline** | Arbre ordonne par date |
| Bordereau de pieces | **Index des sources** | Liste numerotee et horodatee |
| Refere (urgence) | **Priorite** | Flag necesitant resolution acceleree |
| Expertise | **Analyse specialisee** | Workflow operateur domaine-specifique |
| Temoin | **Contributeur** | Auteur d'un noeud |
| Serment | **Signature** | Engagement cryptographique d'integrite |
| Greffe | **Journal** | Table d'audit |
| Scelle | **Hash** | Preuve d'integrite a un instant donne |

---

## 7. RAG structure en 5W1H

### 7.1 Principe

Chaque source versee dans un arbre est decomposee en ses 6 dimensions au moment
de l'indexation :

| Dimension | Ce qu'on extrait |
|-----------|------------------|
| **Qui** (Who) | Acteurs, entites, parties, roles |
| **Quoi** (What) | Evenement, action, decision |
| **Quand** (When) | Date, periode, sequence temporelle |
| **Ou** (Where) | Lieu, juridiction, contexte geographique |
| **Pourquoi** (Why) | Cause, motivation, fondement |
| **Comment** (How) | Mecanisme, procedure, moyen |

### 7.2 Requetes dimensionnelles

La requete de l'avocat dans le horostracker du client n'est plus "trouve quelque
chose de similaire". C'est une requete dimensionnelle :

- "Qui a signe le contrat ?" → dimension Qui
- "Quand le preavis a-t-il ete envoye ?" → dimension Quand
- "Pourquoi le bailleur invoque-t-il un motif legitime ?" → dimension Pourquoi

### 7.3 Vues du SPA

Le SPA devient un explorateur dimensionnel :

| Vue | Contenu |
|-----|---------|
| **Qui** | Acteurs, roles, sources qui les mentionnent, contradictions |
| **Quand** | Timeline reconstruite, trous et incoherences temporelles |
| **Ou** | Contexte geographique, droit applicable |
| **Pourquoi** | Chaine causale, motivations, fondements, contradictions |
| **Comment** | Mecanismes, procedures suivies ou violees |
| **Quoi** | Synthese : etat courant avec profil de vulnerabilite |

### 7.4 Stack RAG multi-couche

| Couche | Type | Fonction |
|--------|------|----------|
| 1 | **5W1H** | Decomposition dimensionnelle a l'indexation |
| 2 | **Entity Linking** | Desambiguation d'entites (ELERAG) |
| 3 | **Temporal Graph** | Versionnement temporel (T-GRAG) |
| 4 | **Spatial Filtering** | Projection juridictionnelle (Spatial-RAG) |
| 5 | **Adversarial Retrieval** | Retrieval bi-objectif (pour + contre) |
| 6 | **Case-Based** | Analogie entre dossiers (CBR-RAG) |

### 7.5 Adversarial-RAG

Differenciateur : le retrieval n'est pas mono-objectif (trouver le plus pertinent)
mais **bi-objectif** — trouver le plus pertinent pour ET le plus pertinent contre.
Chaque requete produit deux ensembles de resultats.

Personne ne fait du retrieval dimensionnel adversarial : "trouve les sources qui
contredisent cette assertion sur la dimension temporelle specifiquement."

---

## 8. Le sas d'investigation collaboratif

### 8.1 Positionnement

Pas un SaaS de RAG custom. Pas un logiciel de gestion de dossier. Un **sas
d'investigation collaboratif** open source, avec LLM gratuits.

Le vrai concurrent n'est pas le legal tech — c'est Google + ChatGPT. Meme
accessibilite (gratuit, depuis un navigateur) mais avec la rigueur structurelle.

### 8.2 Le sas comme espace de transition

**Entree** : documents en vrac, situation confuse, question vague.

**Sortie** : assertions atomiques identifiees, sources indexees en 5W1H, profil
de vulnerabilite par assertion, faiblesses identifiees, sources manquantes listees,
arbres publics pertinents rattaches. Le tout dans un SQLite portable.

**Entre les deux** : les LLM gratuits decomposent, structurent, attaquent. La
communaute a deja mache les questions courantes. Les operateurs ont construit des
workflows de deconstruction par domaine.

### 8.3 Effet reseau

Plus il y a de dossiers traites, plus les arbres publics sont complets, plus chaque
nouveau justiciable entre avec un avantage. L'asymetrie d'information s'inverse.

### 8.4 Investigation, pas decision

Le sas ne tranche pas. Il dit : "voici l'etat de votre dossier. Vos points forts
sont X et Y. Vos faiblesses sont Z. Il vous manque telle source pour consolider
tel point."

Ce n'est pas du conseil juridique. C'est de la structuration d'information soumise
au contradictoire.

---

## 9. Architecture usager-centree

### 9.1 Inversion du flux de donnees

Le dossier vit chez l'usager. L'instance de l'usager est la source de verite.
L'avocat se connecte au horostracker du client. Si l'avocat change, le client
revoque et re-invite. Le dossier n'a pas bouge.

### 9.2 ACL par arbre

| Concept | Implementation |
|---------|----------------|
| Invitation | Acces nominatif : arbre(s), role, duree |
| Role conseil | Lire, poser des questions, verser des sources, annoter |
| Revocation | Retrait d'acces, horodate, immutable dans le journal |
| Invitation deanonymisee | L'invite voit les vrais noms (decodage cote client via table KV) |

Un usager peut avoir N arbres partages avec N invites differents, sans qu'aucun
ne voie les autres dossiers.

### 9.3 Connectivites positives

- L'arbre prive peut avoir des **liens vers des arbres publics** (jurisprudence
  collective, questions de droit resolues).
- Quand un invite identifie une question non resolue publiquement, il peut la poser
  en zone publique (anonymisee).
- Le travail individuel alimente le commun et le commun alimente le travail
  individuel.

### 9.4 Portabilite

L'instance = un binaire + un SQLite. Le dossier tient dans un fichier. Export,
copie sur cle USB, restauration sur autre instance. Schema public, documente,
verifiable. Pas de vendor lock-in.

---

## 10. Anonymisation a la charge de l'usager

### 10.1 Principe

L'anonymisation est la responsabilite legale de l'usager. Le systeme fournit
l'outillage (extraction automatique, proposition d'alias, table KV). L'usager
valide et publie.

### 10.2 Flux

```
Usager ajoute une source
    |
    v
Extraction 5W1H automatique
    |
    v
Dimension "Qui" → personnes physiques, personnes morales
Dimension "Ou"  → lieux
    |
    v
Creation/mise a jour de la table KV locale
    Key: entite reelle
    Value: alias propose (auto-genere)
    |
    v
Usager revoit le mappage
    - Accepter l'alias propose
    - Changer l'alias
    - Marquer comme "public" (entite publique, pas d'anonymisation)
    |
    v
Deux versions coexistent :
    - Version privee (vrais noms) → instance de l'usager, jamais transmise
    - Version anonymisee (alias) → providers, arbres publics, exports
```

### 10.3 Persistance des alias

Un meme acteur dans 12 sources differentes recoit le meme alias. Entity resolution
avant anonymisation. Si une 13e source mentionne le meme acteur, le systeme detecte
et applique l'alias existant, avec validation de l'usager.

### 10.4 Lieux : deux niveaux

| Niveau | Traitement |
|--------|------------|
| Adresse precise | Anonymisee systematiquement ("Adresse X") |
| Juridiction / zone reglementaire | Conservee en clair ("zone tendue", "ressort TJ de Lyon") |

L'usager controle le curseur. La responsabilite lui revient.

### 10.5 Table KV

- Ne quitte jamais l'instance
- Chiffree au repos
- Seul lien entre alias et reel
- C'est aussi un index d'entites resolues
- N'est pas transmise aux providers

### 10.6 Invitation deanonymisee

L'invite (avocat) voit les vrais noms parce que le **client de l'usager** applique
la table KV au moment de l'affichage. Les requetes aux providers passent toujours
en version anonymisee. La deanonymisation est cote client, pas cote serveur.

### 10.7 Positionnement legal

- L'usager est le responsable de traitement au sens RGPD
- Horostracker fournit l'outil, l'usager valide et publie
- Meme modele que les plateformes de publication (YouTube fournit l'upload,
  l'usager est responsable du contenu)
- Si un alias est insuffisamment anonymisant, c'est la responsabilite de l'usager

### 10.8 Donnees emergentes

La table KV, agregee sur des milliers d'usagers (sans les valeurs, juste les cles
typees), produit une **ontologie des roles** : combien de dossiers impliquent un
"Bailleur", un "Employeur", un "Assureur". Indique aux operateurs quels workflows
developper en priorite.

### 10.9 Dossier-temoin

Un usager qui a gagne son contentieux peut publier son arbre complet en version
anonymisee comme cas d'ecole. Reel, source, resolu, avec tout le parcours — mais
anonyme. Training data pour les workflows ET aide directe pour les justiciables
suivants.

---

## 11. Mappage international

### 11.1 Schema neutre

Horostracker ne stocke pas un dossier "francais" ou "americain". Il stocke un arbre
de noeuds types (assertions, sources, contradictions) avec metadonnees. C'est
universel.

### 11.2 Couche de projection par juridiction

Un fichier de configuration par juridiction definit :

| Element | Contenu |
|---------|---------|
| **Labels** | Traduction des types de noeuds dans le vocabulaire local |
| **Regles de pertinence** | Filtres sur les sources (recevabilite) |
| **Templates d'export** | Format de sortie (conclusions FR, brief US, Schriftsatz DE) |
| **Connecteurs proceduraux** | Metadonnees specifiques (numero de role, chambre) |

### 11.3 Les 5W1H comme pont universel

Les 5W1H sont universels. Un bail francais et un lease agreement americain repondent
aux memes questions avec des formats differents. Le squelette 5W1H est le meme,
les regles de chaque dimension changent par juridiction.

### 11.4 Flywheel

Chaque juridiction ajoutee augmente la valeur du reseau pour toutes les autres.
Le registre de juridictions est lui-meme un arbre horostracker — auto-referentiel
de maniere productive.

---

## 12. Application audience temps reel (vision)

Un flux temps reel pour les audiences :

```
Audio (audience)
    → Speech-to-text (Whisper)
    → Extraction d'assertions (LLM decomposeur)
    → Confrontation au dossier (RAG sur corpus ferme)
    → Divergence detectee → affichage silencieux
```

Quatre niveaux de detection :

| Niveau | Type |
|--------|------|
| 1 | Contradiction factuelle (date, montant) |
| 2 | Deformation de jurisprudence |
| 3 | Omission de source versee au dossier |
| 4 | Incoherence interne (contradictions pendant l'audience) |

> Note : cette reflexion a donne lieu a un projet separe — **touchstone registry**.
