package contracts

type CommandType string

const (
	CommandPrompt   CommandType = "prompt"
	CommandLocal    CommandType = "local"
	CommandLocalJSX CommandType = "local-jsx"
)

type CommandSource string

const (
	CommandSourceBuiltin CommandSource = "builtin"
	CommandSourceSkills  CommandSource = "skills"
	CommandSourcePlugin  CommandSource = "plugin"
	CommandSourceBundled CommandSource = "bundled"
	CommandSourceMCP     CommandSource = "mcp"
)

type Command struct {
	Type                    CommandType    `json:"type"`
	Name                    string         `json:"name"`
	Aliases                 []string       `json:"aliases,omitempty"`
	DisplayName             string         `json:"display_name,omitempty"`
	Description             string         `json:"description,omitempty"`
	ArgumentHint            string         `json:"argument_hint,omitempty"`
	ArgumentNames           []string       `json:"argument_names,omitempty"`
	Source                  CommandSource  `json:"source,omitempty"`
	LoadedFrom              string         `json:"loaded_from,omitempty"`
	SkillRoot               string         `json:"skill_root,omitempty"`
	DisableModelInvocation  bool           `json:"disable_model_invocation,omitempty"`
	DisableNonInteractive   bool           `json:"disable_non_interactive,omitempty"`
	SupportsNonInteractive  bool           `json:"supports_non_interactive,omitempty"`
	Immediate               bool           `json:"immediate,omitempty"`
	Sensitive               bool           `json:"sensitive,omitempty"`
	Hidden                  bool           `json:"hidden,omitempty"`
	AllowedTools            []string       `json:"allowed_tools,omitempty"`
	WhenToUse               string         `json:"when_to_use,omitempty"`
	Version                 string         `json:"version,omitempty"`
	Model                   string         `json:"model,omitempty"`
	Context                 string         `json:"context,omitempty"`
	Agent                   string         `json:"agent,omitempty"`
	Effort                  string         `json:"effort,omitempty"`
	Paths                   []string       `json:"paths,omitempty"`
	ContentLength           int            `json:"content_length,omitempty"`
	ProgressMessage         string         `json:"progress_message,omitempty"`
	Availability            []string       `json:"availability,omitempty"`
	UserConfig              map[string]any `json:"user_config,omitempty"`
	HasUserSpecifiedDetails bool           `json:"has_user_specified_details,omitempty"`
}
