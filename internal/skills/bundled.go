package skills

// SKILL-23: Bundled skills — programmatically-registered built-in skills that
// ship with the CLI binary and are available in all projects without any
// .claude/skills/ directory.
//
// CC ref: src/skills/bundledSkills.ts (registerBundledSkill / getBundledSkills).

import (
	"sync"

	"ccgo/internal/contracts"
)

// BundledSkillDefinition describes a built-in skill that is registered
// programmatically at startup. Mirrors CC's BundledSkillDefinition type
// (skills/bundledSkills.ts:BundledSkillDefinition).
type BundledSkillDefinition struct {
	Name                   string
	Description            string
	Aliases                []string
	WhenToUse              string
	ArgumentHint           string
	AllowedTools           []string
	Model                  string
	DisableModelInvocation bool
	// UserInvocable controls whether the skill appears in user-facing lists.
	// nil means default (true — visible).
	UserInvocable *bool
	// Content is the static skill prompt text (replaces the SKILL.md body).
	Content string
}

var (
	bundledMu     sync.Mutex
	bundledSkills []contracts.Command
)

// RegisterBundledSkill adds a built-in skill to the global bundled skill
// registry. It is safe to call from init() functions.
// CC ref: skills/bundledSkills.ts:registerBundledSkill.
func RegisterBundledSkill(def BundledSkillDefinition) {
	userInvocable := true
	if def.UserInvocable != nil {
		userInvocable = *def.UserInvocable
	}
	cmd := contracts.Command{
		Type:                   contracts.CommandPrompt,
		Name:                   def.Name,
		Description:            def.Description,
		Aliases:                append([]string(nil), def.Aliases...),
		WhenToUse:              def.WhenToUse,
		ArgumentHint:           def.ArgumentHint,
		AllowedTools:           append([]string(nil), def.AllowedTools...),
		Model:                  def.Model,
		DisableModelInvocation: def.DisableModelInvocation,
		Hidden:                 !userInvocable,
		Source:                 contracts.CommandSourceBundled,
		LoadedFrom:             "bundled",
		ProgressMessage:        "running",
		HasUserSpecifiedDetails: def.Description != "" || def.WhenToUse != "",
	}

	bundledMu.Lock()
	bundledSkills = append(bundledSkills, cmd)
	bundledMu.Unlock()
}

// GetBundledSkills returns a snapshot copy of all registered bundled skills.
// CC ref: skills/bundledSkills.ts:getBundledSkills.
func GetBundledSkills() []contracts.Command {
	bundledMu.Lock()
	out := make([]contracts.Command, len(bundledSkills))
	copy(out, bundledSkills)
	bundledMu.Unlock()
	return out
}

// ClearBundledSkills empties the registry. Intended for use in tests only.
// CC ref: skills/bundledSkills.ts:clearBundledSkills.
func ClearBundledSkills() {
	bundledMu.Lock()
	bundledSkills = nil
	bundledMu.Unlock()
}
