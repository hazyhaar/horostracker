package llm

// CoreFlows returns the 5 built-in thinking flow configurations per spec.
// These can be overridden by TOML config files.
func CoreFlows() []FlowConfig {
	return []FlowConfig{
		flowConfrontation(),
		flowRedTeam(),
		flowFidelityBenchmark(),
		flowAdversarialDetection(),
		flowDeepDive(),
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
