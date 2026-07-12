package clitest

import (
	"fmt"
	"strings"

	"braid/internal/cli"
)

type CompletionCase struct {
	Name      string
	Words     []string
	Want      []string
	Unwanted  []string
	WantEmpty bool
}

type SyntaxCase struct {
	Name      string
	Args      []string
	WantError string
}

func SyntaxConstraintCases() []SyntaxCase {
	var cases []SyntaxCase
	for _, spec := range cli.CommandSpecs() {
		for _, requirement := range spec.RequiresPositionals {
			baseMin := requiredPositionals(spec.Positionals)
			if baseMin >= requirement.Minimum {
				continue
			}
			for _, optionName := range requirement.Options {
				option, _ := spec.Option(optionName)
				invalid := []string{spec.Name, option.Long}
				if option.TakesValue() {
					invalid = append(invalid, "value")
				}
				for i := 0; i < baseMin; i++ {
					invalid = append(invalid, samplePositional(spec.Positionals[i]))
				}
				valid := append([]string(nil), invalid...)
				for i := baseMin; i < requirement.Minimum; i++ {
					valid = append(valid, samplePositional(spec.Positionals[i]))
				}
				cases = append(cases,
					SyntaxCase{Name: spec.Name + " requires positional " + option.Long, Args: invalid, WantError: requirement.Error},
					SyntaxCase{Name: spec.Name + " accepts positional " + option.Long, Args: valid},
				)
			}
		}
	}
	return cases
}

func CompletionContractCases() []CompletionCase {
	var cases []CompletionCase
	var rootCandidates []string
	for _, spec := range cli.CommandSpecs() {
		if spec.Hidden {
			continue
		}
		rootCandidates = append(rootCandidates, spec.Name)
		rootCandidates = append(rootCandidates, spec.Aliases...)
	}
	helpSpec := cli.Help()
	cases = append(cases, CompletionCase{Name: "root commands", Words: []string{""}, Want: append(rootCandidates, helpSpec.Command), Unwanted: []string{"__complete"}})
	for _, help := range helpSpec.Names() {
		cases = append(cases, CompletionCase{Name: "root terminal help " + help, Words: []string{help, ""}, WantEmpty: true})
	}

	for _, option := range cli.GlobalOptionsSpec() {
		for _, name := range optionNames(option) {
			cases = append(cases, CompletionCase{Name: "global option " + name, Words: []string{name}, Want: []string{name}})
		}
		if option.TakesValue() {
			cases = append(cases,
				CompletionCase{Name: "global separate value " + option.Long, Words: []string{option.Long, ""}, WantEmpty: option.Completion != cli.CompletionDirectory},
				CompletionCase{Name: "global inline value " + option.Long, Words: []string{option.Long + "="}},
			)
		}
	}
	for _, conflict := range cli.GlobalConflictsSpec() {
		if len(conflict.Options) < 2 {
			continue
		}
		first, second := conflict.Options[0], conflict.Options[1]
		words := []string{first}
		for _, option := range cli.GlobalOptionsSpec() {
			if option.Long == first && option.TakesValue() {
				words = append(words, "value")
			}
		}
		words = append(words, "--")
		cases = append(cases, CompletionCase{Name: "global conflict " + strings.Join(conflict.Options, " "), Words: words, Unwanted: []string{second}})
	}

	for _, spec := range cli.CommandSpecs() {
		if spec.Hidden {
			continue
		}
		for _, commandName := range append([]string{spec.Name}, spec.Aliases...) {
			cases = append(cases, commandCases(spec, commandName)...)
		}
	}
	return cases
}

func commandCases(spec cli.CommandSpec, commandName string) []CompletionCase {
	var cases []CompletionCase
	helpSpec := cli.Help()
	for _, help := range helpSpec.Names() {
		cases = append(cases, CompletionCase{Name: commandName + " terminal help " + help, Words: []string{commandName, help, ""}, WantEmpty: true})
	}
	for boundary, positionals := range positionalBoundaries(spec.Positionals) {
		words := append([]string{commandName}, positionals...)
		words = append(words, "")
		var want []string
		for _, option := range spec.Options {
			if option.Availability.Allows(positionals) {
				want = append(want, optionNames(option)...)
			}
		}
		if spec.Passthrough != nil {
			want = append(want, "--")
		}
		if boundary == 0 {
			want = append(want, helpSpec.Names()...)
			if len(spec.Positionals) > 0 && spec.Positionals[0].Completion == cli.CompletionStatic {
				want = append(want, spec.Positionals[0].Values...)
			}
		}
		cases = append(cases, CompletionCase{Name: fmt.Sprintf("%s boundary %d", commandName, boundary), Words: words, Want: want})
	}
	for _, option := range spec.Options {
		if option.TakesValue() {
			for _, name := range optionNames(option) {
				cases = append(cases,
					CompletionCase{Name: commandName + " separate value " + name, Words: []string{commandName, name, ""}, WantEmpty: true},
					CompletionCase{Name: commandName + " inline value " + name, Words: []string{commandName, name + "="}, WantEmpty: true},
				)
			}
		}
		if option.Availability.ForbiddenPrefix != "" {
			positionals := make([]string, option.Availability.PositionalIndex+1)
			positionals[option.Availability.PositionalIndex] = option.Availability.ForbiddenPrefix + "value"
			words := append([]string{commandName}, positionals...)
			words = append(words, "--")
			cases = append(cases, CompletionCase{Name: commandName + " availability " + option.Long, Words: words, Unwanted: optionNames(option)})
		}
	}
	for _, conflict := range spec.Conflicts {
		if len(conflict.Options) < 2 {
			continue
		}
		first, second := conflict.Options[0], conflict.Options[1]
		words := []string{commandName, first}
		if option, ok := spec.Option(first); ok && option.TakesValue() {
			words = append(words, "value")
		}
		words = append(words, "--")
		option, _ := spec.Option(second)
		cases = append(cases, CompletionCase{Name: commandName + " conflict " + strings.Join(conflict.Options, " "), Words: words, Unwanted: optionNames(option)})
	}
	return cases
}

func positionalBoundaries(positionals []cli.PositionalSpec) [][]string {
	boundaries := [][]string{{}}
	var completed []string
	for _, positional := range positionals {
		completed = append(completed, samplePositional(positional))
		boundaries = append(boundaries, append([]string(nil), completed...))
		if positional.Repeatable {
			completed = append(completed, samplePositional(positional)+"-2")
			boundaries = append(boundaries, append([]string(nil), completed...))
		}
	}
	return boundaries
}

func samplePositional(positional cli.PositionalSpec) string {
	if len(positional.Values) > 0 {
		return positional.Values[0]
	}
	switch positional.Completion {
	case cli.CompletionMirror:
		return "mirror"
	case cli.CompletionFilesystem:
		return "path"
	default:
		return "value"
	}
}

func requiredPositionals(positionals []cli.PositionalSpec) int {
	count := 0
	for _, positional := range positionals {
		if positional.Required {
			count++
		}
	}
	return count
}

func optionNames(option cli.OptionSpec) []string {
	names := []string{option.Long}
	if option.Short != "" {
		names = append(names, option.Short)
	}
	return names
}
