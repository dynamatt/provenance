package validate

// Check is a built-in check (DES-0048): what the JUnit and SARIF reports
// list as a rule, passed or not.
type Check struct {
	ID       string
	Severity Severity
	// Description is the check's short description, the title of its
	// ErrorClass in testdata/violations.
	Description string
}

// Checks are every built-in check, by ID. TestChecksMatchTheCatalogue keeps
// them in step with the checks the code reports and with the catalogue.
var Checks = []Check{
	{"content/caption", Error, "A caption that cannot be placed or referred to"},
	{"content/duplicate-id", Error, "Two files with one ID"},
	{"content/embed", Error, "An embed inside running text"},
	{"content/embed-cycle", Error, "Entities that embed each other"},
	{"content/format", Warning, "An entity file not in canonical form"},
	{"content/frontmatter", Error, "Frontmatter that cannot be read"},
	{"content/image", Error, "An image the website cannot publish"},
	{"content/query", Error, "A query block that cannot run"},
	{"content/template", Error, "A template that cannot render"},
	{"content/template-name", Warning, "A template named for nothing"},
	{"content/unknown-override", Warning, "An underscore template that overrides nothing"},
	{"content/unrendered-block", Error, "A block this version publishes only as source"},
	{"content/unused-template", Warning, "A named template no query chooses"},
	{"links/target-type", Error, "A link to an entity of the wrong type"},
	{"links/unknown-field", Error, "A field reference to a field the target does not have"},
	{"links/unresolved", Error, "A link to no entity"},
	{"rules/file", Error, "A rule file that is not a YAML mapping"},
	{"rules/message", Error, "A rule without a message"},
	{"rules/name", Error, "A rule file not named for a kebab-case ID"},
	{"rules/query", Error, "A rule whose query does not fit the schema"},
	{"rules/severity", Error, "A rule severity other than error or warning"},
	{"rules/unknown-key", Error, "A key a rule file does not have"},
	{"schema/calculated-set", Error, "A calculated field written in the file"},
	{"schema/enum-value", Error, "A value not in its enum"},
	{"schema/file", Error, "A schema file that cannot be loaded"},
	{"schema/formula", Error, "A calculated field whose formula cannot run"},
	{"schema/id-format", Error, "An ID that is not the type's form"},
	{"schema/invalid-value", Error, "A value not of its field's type"},
	{"schema/link-target", Error, "A link field targeting an undeclared type"},
	{"schema/list-row", Error, "A list row with a key its list does not have"},
	{"schema/required", Error, "A required field not set"},
	{"schema/unknown-field", Error, "A frontmatter key the type does not have"},
	{"schema/unknown-key", Error, "A key the schema language does not have"},
	{"schema/unknown-type", Error, "An entity of an undeclared type"},
	{"schema/unused-enum", Warning, "An enum no field uses"},
	{"schema/unused-record", Warning, "A record no list uses"},
	{"schema/unused-type", Warning, "A type with no entities and no links"},
}

// RuleInfo is a rule file: run, or skipped because the schema or the file
// itself has errors.
type RuleInfo struct {
	ID, Path string
	// Severity and Message are "" when the file does not say.
	Severity Severity
	Message  string
	// Skipped says why the rule was not run, or is "".
	Skipped string
	// Source is the rule file's text, with LF line endings.
	Source string
}
