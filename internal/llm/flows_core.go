// CLAUDE:SUMMARY Built-in flow definitions (6 legacy flows) and seeding of 14 VACF reference workflows
package llm

import (
	"log/slog"

	"github.com/hazyhaar/horostracker/internal/db"
)

// CoreFlows returns the 6 built-in thinking flow configurations per spec.
// Kept for backward compatibility with the legacy ChallengeRunner.
func CoreFlows() []FlowConfig {
	return []FlowConfig{
		flowConfrontation(),
		flowRedTeam(),
		flowFidelityBenchmark(),
		flowAdversarialDetection(),
		flowDeepDive(),
		flowSafetyScoring(),
	}
}

// SeedCoreWorkflows inserts the 14 VACF reference workflows into the dynamic workflow
// system. This is idempotent — existing workflows are skipped via INSERT OR IGNORE on name.
func SeedCoreWorkflows(flowsDB *db.FlowsDB, botUserID string, logger *slog.Logger) {
	seeds := coreWorkflowSeeds(botUserID)
	seeded := 0
	for _, seed := range seeds {
		// Check if workflow already exists
		var count int
		_ = flowsDB.QueryRow(`SELECT COUNT(*) FROM workflows WHERE name = ?`, seed.wf.Name).Scan(&count)
		if count > 0 {
			continue
		}

		if err := flowsDB.CreateWorkflow(seed.wf); err != nil {
			logger.Warn("seed workflow insert", "name", seed.wf.Name, "error", err)
			continue
		}
		for i := range seed.steps {
			if err := flowsDB.CreateStep(&seed.steps[i]); err != nil {
				logger.Warn("seed workflow step insert", "workflow", seed.wf.Name, "step", seed.steps[i].StepName, "error", err)
			}
		}
		seeded++
	}

	// Seed the viability criteria list
	var clCount int
	_ = flowsDB.QueryRow(`SELECT COUNT(*) FROM criteria_lists WHERE name = 'workflow_viability_thresholds'`).Scan(&clCount)
	if clCount == 0 {
		_ = flowsDB.CreateCriteriaList(&db.CriteriaList{
			ListID:      db.NewID(),
			Name:        "workflow_viability_thresholds",
			Description: "Threshold criteria for workflow validation audit",
			ItemsJSON:   `["Estimated token consumption below 500k per run","No circular prompt references detected","No prompt injection patterns detected","Step types consistent with declared workflow type","Viability score above 60"]`,
			OwnerID:     botUserID,
		})
	}

	if seeded > 0 {
		logger.Info("seeded core workflows", "count", seeded)
	}
}

type workflowSeed struct {
	wf    *db.Workflow
	steps []db.WorkflowStep
}

func coreWorkflowSeeds(botUserID string) []workflowSeed {
	return []workflowSeed{
		seedDecompose(botUserID),
		seedCritique(botUserID),
		seedSource(botUserID),
		seedFactcheck(botUserID),
		seedAnalyse(botUserID),
		seedSynthese(botUserID),
		seedReformulation(botUserID),
		seedMediaExport(botUserID),
		seedContradictionDetection(botUserID),
		seedCompletude(botUserID),
		seedTraduction(botUserID),
		seedClassificationEpistemique(botUserID),
		seedWorkflowValidation(botUserID),
		seedModelDiscovery(botUserID),
	}
}

func mkWF(id, name, desc, wfType, ownerID string) *db.Workflow {
	return &db.Workflow{
		WorkflowID:   id,
		Name:         name,
		Description:  desc,
		WorkflowType: wfType,
		OwnerID:      ownerID,
		OwnerRole:    "operator",
		Status:       "active",
		Version:      1,
	}
}

func mkStep(wfID string, order int, name, stype, provider, model, prompt, system string) db.WorkflowStep {
	return db.WorkflowStep{
		StepID:         db.NewID(),
		WorkflowID:     wfID,
		StepOrder:      order,
		StepName:       name,
		StepType:       stype,
		Provider:       provider,
		Model:          model,
		PromptTemplate: prompt,
		SystemPrompt:   system,
		ConfigJSON:     "{}",
		TimeoutMs:      30000,
		RetryMax:       2,
	}
}

func seedDecompose(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "decompose", "Decompose a claim into thesis/antithesis sub-claims", "decompose", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "decompose", "llm", "groq", "llama-3.3-70b-versatile",
				"Decompose the following claim into sub-claims (thesis and antithesis). List each sub-claim on a separate line prefixed with [THESIS] or [ANTITHESIS].\n\nClaim: {{.Body}}",
				"You are an analytical decomposition engine. Break claims into atomic thesis/antithesis pairs."),
			mkStep(wfID, 2, "validate_decomposition", "check", "", "", "", ""),
		},
	}
}

func seedCritique(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "critique", "Multi-model critical analysis", "critique", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "argue", "llm", "", "",
				"Build the strongest possible argument for this position:\n\n{{.Body}}",
				"You are an expert advocate. Build a compelling case with evidence and reasoning."),
			mkStep(wfID, 2, "counter_argue", "llm", "groq", "llama-3.3-70b-versatile",
				"The following argument was made:\n\n{{.PreviousResponse}}\n\nOriginal claim: {{.Body}}\n\nProvide a thorough counter-argument.",
				"You are a critical analyst. Find every weakness, assumption, and counter-evidence."),
			mkStep(wfID, 3, "synthesize", "llm", "gemini", "gemini-2.0-flash",
				"Original: {{.Body}}\n\nArgument: {{.Step.argue}}\n\nCounter: {{.Step.counter_argue}}\n\nSynthesize into a balanced analysis.",
				"You are a neutral synthesizer."),
		},
	}
}

func seedSource(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "source", "Source identification and evaluation", "source", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "identify_sources", "llm", "", "",
				"Identify relevant sources (academic papers, official data, reputable articles) for:\n\n{{.Body}}\n\nList each source with URL if known.",
				"You are a research librarian. Identify the most reliable sources."),
			mkStep(wfID, 2, "evaluate_reliability", "llm", "gemini", "gemini-2.0-flash",
				"Sources identified:\n{{.PreviousResponse}}\n\nOriginal topic: {{.Body}}\n\nEvaluate each source's reliability (0-100). Flag any potentially fabricated or unreliable sources.",
				"You are a source credibility evaluator."),
		},
	}
}

func seedFactcheck(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "factcheck", "Factual verification", "factcheck", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "extract_claims", "llm", "", "",
				"Extract all verifiable factual claims from the following text:\n\n{{.Body}}\n\nList each claim separately.",
				"You are a fact extraction specialist. Identify only claims that can be verified."),
			mkStep(wfID, 2, "verify_claims", "llm", "groq", "llama-3.3-70b-versatile",
				"Verify each of the following claims. For each, indicate TRUE, FALSE, or UNVERIFIABLE with evidence:\n\n{{.PreviousResponse}}",
				"You are a fact-checker. Be rigorous and cite your sources."),
			mkStep(wfID, 3, "validate_factual", "check", "", "", "", ""),
		},
	}
}

func seedAnalyse(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "analyse", "Iterative deep analysis", "analyse", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "initial_analysis", "llm", "", "",
				"Provide a thorough initial analysis of:\n\n{{.Body}}\n\nIdentify the top 3 areas needing deeper investigation.",
				"You are a thorough analyst. Flag uncertainties explicitly."),
			mkStep(wfID, 2, "deepen", "llm", "groq", "llama-3.3-70b-versatile",
				"Deepen the investigation on the weak points identified:\n\n{{.PreviousResponse}}\n\nOriginal topic: {{.Body}}",
				"You specialize in investigating weak points of analyses."),
			mkStep(wfID, 3, "consolidate", "llm", "gemini", "gemini-2.0-flash",
				"Original: {{.Body}}\n\nInitial: {{.Step.initial_analysis}}\n\nDeepened: {{.Step.deepen}}\n\nConsolidate into a comprehensive answer. Mark remaining uncertainties.",
				"You consolidate research into comprehensive answers."),
		},
	}
}

func seedSynthese(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "synthese", "Synthesize argumentative tree into resolution", "synthese", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "load_tree", "sql", "", "",
				"SELECT body, node_type, score, depth FROM nodes WHERE root_id = '{{.Body}}' ORDER BY depth, created_at LIMIT 100", ""),
			mkStep(wfID, 2, "generate_resolution", "llm", "", "",
				"Based on this argumentative tree data:\n\n{{.PreviousResponse}}\n\nGenerate a structured Resolution (dialogue between argumentative lines, not people).",
				"You are a Resolution generator for a knowledge refinery."),
			mkStep(wfID, 3, "validate_completude", "check", "", "", "", ""),
		},
	}
}

func seedReformulation(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "reformulation", "Rewrite content according to target format", "reformulation", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "reformulate", "llm", "", "",
				"{{.PrePrompt}}\n\nReformulate the following content:\n\n{{.Body}}",
				"You reformulate content while preserving meaning. Follow the format instructions precisely."),
			mkStep(wfID, 2, "validate_fidelity", "check", "", "", "", ""),
		},
	}
}

func seedMediaExport(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "media_export", "Generate exportable media content", "media_export", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "generate_export", "llm", "", "",
				"{{.PrePrompt}}\n\nGenerate a formatted export of:\n\n{{.Body}}",
				"You generate clean, professional content ready for publication."),
			mkStep(wfID, 2, "validate_format", "check", "", "", "", ""),
		},
	}
}

func seedContradictionDetection(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "contradiction_detection", "Detect internal contradictions in argumentation", "contradiction_detection", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "load_claims", "sql", "", "",
				"SELECT body, node_type, score FROM nodes WHERE root_id = '{{.Body}}' AND node_type = 'claim' ORDER BY depth LIMIT 200", ""),
			mkStep(wfID, 2, "detect_contradictions", "llm", "", "",
				"Identify all internal contradictions in the following set of claims:\n\n{{.PreviousResponse}}\n\nFor each contradiction, cite the two conflicting claims and explain the inconsistency.",
				"You are a contradiction detection specialist. Be precise about which claims conflict."),
			mkStep(wfID, 3, "validate_confidence", "check", "", "", "", ""),
		},
	}
}

func seedCompletude(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "completude", "Evaluate argumentative coverage", "completude", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "load_tree", "sql", "", "",
				"SELECT body, node_type, score, depth FROM nodes WHERE root_id = '{{.Body}}' ORDER BY depth LIMIT 200", ""),
			mkStep(wfID, 2, "identify_gaps", "llm", "", "",
				"Analyze this proof tree for gaps in coverage:\n\n{{.PreviousResponse}}\n\nIdentify: missing perspectives, unaddressed counterarguments, unexplored dimensions.",
				"You evaluate argumentative completeness. Identify what's missing."),
			mkStep(wfID, 3, "validate_coverage", "check", "", "", "", ""),
		},
	}
}

func seedTraduction(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "traduction", "Multilingual translation with back-translation verification", "traduction", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "translate", "llm", "", "",
				"{{.PrePrompt}}\n\nTranslate the following text:\n\n{{.Body}}",
				"You are a professional translator. Preserve meaning, tone, and nuance."),
			mkStep(wfID, 2, "back_translate", "llm", "groq", "llama-3.3-70b-versatile",
				"Back-translate the following text to the original language:\n\n{{.PreviousResponse}}",
				"You back-translate to verify translation fidelity."),
			mkStep(wfID, 3, "validate_fidelity", "check", "", "", "", ""),
		},
	}
}

func seedClassificationEpistemique(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "classification_epistemique", "Epistemological classification (Toulmin model)", "classification_epistemique", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "classify_toulmin", "llm", "", "",
				"Classify the following argument using the Toulmin model (Claim, Grounds, Warrant, Backing, Qualifier, Rebuttal):\n\n{{.Body}}",
				"You are an epistemology specialist. Apply the Toulmin model rigorously."),
			mkStep(wfID, 2, "validate_classification", "check", "", "", "", ""),
		},
	}
}

func seedWorkflowValidation(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "workflow_validation", "Automated audit of submitted workflows", "workflow_validation", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "load_definition", "sql", "", "",
				"SELECT w.name, w.workflow_type, w.description, ws.step_order, ws.step_name, ws.step_type, ws.provider, ws.model, ws.prompt_template FROM workflows w JOIN workflow_steps ws ON w.workflow_id = ws.workflow_id WHERE w.workflow_id = '{{.Body}}' ORDER BY ws.step_order", ""),
			mkStep(wfID, 2, "audit", "llm", "mistral", "mistral-large-latest",
				"Audit this workflow definition for viability:\n\n{{.PreviousResponse}}\n\nCheck for:\n1. Estimated token consumption (flag if > 500k per run)\n2. Circular prompt references\n3. Prompt injection risks in templates\n4. Consistency of step types with workflow type\n5. Overall viability score (0-100)",
				"You are a workflow security auditor. Be thorough about injection risks and resource abuse."),
			mkStep(wfID, 3, "validate_viability", "check", "", "", "", ""),
		},
	}
}

func seedModelDiscovery(bot string) workflowSeed {
	wfID := db.NewID()
	return workflowSeed{
		wf: mkWF(wfID, "model_discovery", "Discover available LLM models from configured providers", "model_discovery", bot),
		steps: []db.WorkflowStep{
			mkStep(wfID, 1, "discover", "http", "", "",
				"", ""),
		},
	}
}

// Flow 1: Confrontation multi-modèle
// A répond → B objecte avec sources → C synthétise → D tranche
func flowConfrontation() FlowConfig {
	return FlowConfig{
		Name:        "confrontation",
		Description: "Multi-model confrontation: respond → object → synthesize → judge",
		Steps: []FlowStep{
			{
				Name:     "respond",
				Provider: "$TARGET",
				Model:    "$TARGET",
				Role:     "defender",
				System:   "You are an expert analyst. Provide a well-sourced, detailed answer.",
				Prompt:   "Answer the following question with sources and evidence:\n\n{{.Body}}",
			},
			{
				Name:     "object",
				Provider: "groq",
				Model:    "llama-3.3-70b-versatile",
				Role:     "attacker",
				System:   "You are a critical reviewer. Find flaws, missing nuances, and counter-arguments. Cite sources.",
				Prompt:   "The following question was asked:\n\n{{.Body}}\n\nAn answer was provided:\n\n{{.PreviousResponse}}\n\nProvide substantive objections with sources. What is wrong, incomplete, or misleading?",
			},
			{
				Name:     "synthesize",
				Provider: "gemini",
				Model:    "gemini-2.0-flash",
				Role:     "synthesizer",
				System:   "You are a neutral synthesizer. Combine arguments fairly, noting where they agree and disagree.",
				Prompt:   "Original question:\n{{.Body}}\n\nInitial answer:\n{{.Step.respond}}\n\nObjections:\n{{.Step.object}}\n\nSynthesize both positions into a balanced analysis. Note areas of agreement, disagreement, and remaining uncertainty.",
			},
			{
				Name:     "judge",
				Provider: "mistral",
				Model:    "mistral-large-latest",
				Role:     "judge",
				System:   "You are an impartial judge. Evaluate the quality of arguments and deliver a verdict.",
				Prompt:   "Question:\n{{.Body}}\n\nSynthesis of arguments:\n{{.PreviousResponse}}\n\nDeliver a verdict: which position is better supported? Rate confidence 0-100. Identify remaining open questions.",
			},
		},
	}
}

// Flow 2: Red team automatisé
// arguments pour la claim → sources crédibles → démontage → classification
func flowRedTeam() FlowConfig {
	return FlowConfig{
		Name:        "red_team",
		Description: "Automated red teaming: build case → source → demolish → classify deception",
		Steps: []FlowStep{
			{
				Name:     "build_case",
				Provider: "$TARGET",
				Model:    "$TARGET",
				Role:     "defender",
				System:   "You are building the strongest possible case FOR the following claim, even if it may be false. Use persuasive arguments and cite plausible sources.",
				Prompt:   "Build the strongest possible argument supporting this claim:\n\n{{.Body}}",
			},
			{
				Name:     "demolish",
				Provider: "groq",
				Model:    "llama-3.3-70b-versatile",
				Role:     "attacker",
				System:   "You are a fact-checker and critical analyst. Systematically demolish the argument below. Identify every logical fallacy, unsupported claim, misused source, and deceptive technique.",
				Prompt:   "The following argument was constructed in favor of a claim:\n\n{{.PreviousResponse}}\n\nOriginal claim: {{.Body}}\n\nDemolish this argument systematically. For each point, explain why it fails.",
			},
			{
				Name:     "classify",
				Provider: "gemini",
				Model:    "gemini-2.0-flash",
				Role:     "judge",
				System:   "You are a deception classification expert. Categorize the techniques used.",
				Prompt:   "Original claim: {{.Body}}\n\nArgument for the claim:\n{{.Step.build_case}}\n\nDemolition:\n{{.Step.demolish}}\n\nClassify the deception mechanisms used in the original argument. Categories: citation_hors_contexte, extrapolation_abusive, appel_autorite, donnees_truquees, confusion_terminologique, accroche_emotionnelle, grain_de_verite_exploite. Rate overall deceptiveness 0-100.",
			},
		},
	}
}

// Flow 3: Benchmark de fidélité
// arbre résolu → chaque modèle produit une Résolution → comparaison
func flowFidelityBenchmark() FlowConfig {
	return FlowConfig{
		Name:        "fidelity_benchmark",
		Description: "Fidelity benchmark: generate Resolution and compare with reference",
		Steps: []FlowStep{
			{
				Name:     "generate_resolution",
				Provider: "$TARGET",
				Model:    "$TARGET",
				Role:     "synthesizer",
				System:   "You are a Resolution generator for a knowledge refinery. Create a structured dialogue between argumentative lines (not between people). Cover all key points, objections, and evidence from the tree.",
				Prompt:   "Generate a Resolution (structured living dialogue) for the following proof tree:\n\n{{.Body}}",
			},
			{
				Name:     "evaluate_fidelity",
				Provider: "gemini",
				Model:    "gemini-2.0-flash",
				Role:     "judge",
				System:   "You are a fidelity evaluator. Compare a generated Resolution against the source material.",
				Prompt:   "Source material (proof tree):\n{{.Body}}\n\nGenerated Resolution:\n{{.PreviousResponse}}\n\nEvaluate fidelity on these axes (0-100 each):\n1. Completeness — are all key arguments represented?\n2. Accuracy — are claims faithfully reproduced?\n3. Balance — are opposing views fairly weighted?\n4. Source attribution — are sources correctly cited?\n5. Nuance — are uncertainties and caveats preserved?\n\nProvide an overall fidelity score and specific issues found.",
			},
		},
	}
}

// Flow 4: Détection adversariale
// source fabriquée → chaque modèle évalue → score de résistance
func flowAdversarialDetection() FlowConfig {
	return FlowConfig{
		Name:        "adversarial_detection",
		Description: "Adversarial detection: fabricated source → evaluate → resistance score",
		Steps: []FlowStep{
			{
				Name:     "fabricate",
				Provider: "groq",
				Model:    "llama-3.3-70b-versatile",
				Role:     "attacker",
				System:   "You are creating a convincing but fabricated source to test detection capabilities. Make it look real but include subtle inconsistencies that a careful reader would catch.",
				Prompt:   "Create a fabricated but convincing source (fake study, fake article, fake statistics) that supports the following claim:\n\n{{.Body}}\n\nMake it look professional and credible, but include 3-5 subtle red flags.",
			},
			{
				Name:     "detect",
				Provider: "$TARGET",
				Model:    "$TARGET",
				Role:     "defender",
				System:   "You are a source verification expert. Analyze the following source for authenticity. Be thorough and skeptical.",
				Prompt:   "Analyze this source for authenticity. Is it real or fabricated? What red flags do you see?\n\nSource:\n{{.PreviousResponse}}\n\nContext claim: {{.Body}}",
			},
			{
				Name:     "score",
				Provider: "gemini",
				Model:    "gemini-2.0-flash",
				Role:     "judge",
				System:   "You are evaluating detection performance.",
				Prompt:   "A fabricated source was created with these embedded red flags:\n{{.Step.fabricate}}\n\nThe detector's analysis was:\n{{.Step.detect}}\n\nScore the detection (0-100):\n1. Red flags found / total red flags\n2. False positives (things flagged that weren't red flags)\n3. Reasoning quality\n4. Overall resistance score",
			},
		},
	}
}

// Flow 6: Safety scoring communautaire
// Analyse le contenu pour détecter les patterns problématiques et scorer la sûreté
func flowSafetyScoring() FlowConfig {
	return FlowConfig{
		Name:        "safety_scoring",
		Description: "Safety analysis: detect harmful patterns, score content safety, suggest mitigations",
		Steps: []FlowStep{
			{
				Name:     "analyze",
				Provider: "$TARGET",
				Model:    "$TARGET",
				Role:     "defender",
				System:   "You are a content safety analyst. Analyze the following content for potential harms, misinformation, manipulation techniques, and safety concerns.",
				Prompt:   "Analyze the safety of the following content. Identify any harmful patterns, misinformation, manipulation techniques, or safety concerns:\n\n{{.Body}}",
			},
			{
				Name:     "score",
				Provider: "groq",
				Model:    "llama-3.3-70b-versatile",
				Role:     "judge",
				System:   "You are a safety scoring expert. Evaluate the analysis and provide structured safety scores.",
				Prompt:   "Based on the following safety analysis:\n\n{{.PreviousResponse}}\n\nOriginal content:\n{{.Body}}\n\nProvide safety scores (0-100, where 100 is perfectly safe):\n1. Factual accuracy score\n2. Manipulation resistance score\n3. Source credibility score\n4. Overall safety score\n\nList any specific safety flags found.",
			},
			{
				Name:     "mitigate",
				Provider: "gemini",
				Model:    "gemini-2.0-flash",
				Role:     "synthesizer",
				System:   "You are a safety mitigation expert. Suggest concrete improvements based on the safety analysis.",
				Prompt:   "Safety analysis:\n{{.Step.analyze}}\n\nSafety scores:\n{{.Step.score}}\n\nOriginal content:\n{{.Body}}\n\nSuggest specific mitigations to improve the safety of this content. Focus on actionable improvements while preserving the original intent.",
			},
		},
	}
}

// Flow 5: Approfondissement itératif
// identify weak points → deep dive → until exhaustion (2 rounds for now)
func flowDeepDive() FlowConfig {
	return FlowConfig{
		Name:        "deep_dive",
		Description: "Iterative deepening: identify weak points → investigate → deepen",
		Steps: []FlowStep{
			{
				Name:     "initial_analysis",
				Provider: "$TARGET",
				Model:    "$TARGET",
				Role:     "defender",
				System:   "You are a thorough analyst. Provide a complete answer and explicitly flag areas of uncertainty or weak evidence.",
				Prompt:   "Analyze the following question thoroughly. At the end, list the TOP 3 weakest points in your analysis that need deeper investigation:\n\n{{.Body}}",
			},
			{
				Name:     "deepen",
				Provider: "groq",
				Model:    "llama-3.3-70b-versatile",
				Role:     "attacker",
				System:   "You specialize in investigating the weakest points of an analysis. Go deeper on each identified weakness.",
				Prompt:   "The following analysis was provided:\n\n{{.PreviousResponse}}\n\nInvestigate each of the identified weak points. Provide additional evidence, counter-arguments, or corrections for each.",
			},
			{
				Name:     "consolidate",
				Provider: "gemini",
				Model:    "gemini-2.0-flash",
				Role:     "synthesizer",
				System:   "You consolidate research into a final comprehensive answer.",
				Prompt:   "Original question: {{.Body}}\n\nInitial analysis:\n{{.Step.initial_analysis}}\n\nDeep investigation of weak points:\n{{.Step.deepen}}\n\nProduce a consolidated, comprehensive answer that incorporates the deeper investigation. Clearly mark remaining uncertainties.",
			},
		},
	}
}
