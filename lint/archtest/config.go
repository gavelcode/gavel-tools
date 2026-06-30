package archtest

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var ErrInvalidConfig = errors.New("invalid architecture config")

type Config struct {
	Layers       map[string][]string
	Rules        []Rule
	DetectCycles bool
}

type Rule struct {
	Name   string
	Source string
	Deny   []string
}

type configDTO struct {
	Version      int                 `yaml:"version"`
	Module       string              `yaml:"module"`
	Layers       map[string][]string `yaml:"layers"`
	Rules        []ruleDTO           `yaml:"rules"`
	DetectCycles bool                `yaml:"detect_cycles"`
	Generic      genericDTO          `yaml:"generic"`
}

type ruleDTO struct {
	Name   string   `yaml:"name"`
	Source string   `yaml:"source"`
	Deny   []string `yaml:"deny"`
}

type genericDTO struct {
	NoCyclicalDeps bool `yaml:"no_circular_deps"`
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	return parseConfig(data)
}

func parseConfig(data []byte) (Config, error) {
	var parsedConfig configDTO
	if err := yaml.Unmarshal(data, &parsedConfig); err != nil {
		return Config{}, fmt.Errorf("parse YAML: %w", err)
	}

	if len(parsedConfig.Layers) == 0 {
		return Config{}, fmt.Errorf("%w: at least one layer is required", ErrInvalidConfig)
	}

	if isV1(parsedConfig) {
		return buildV1Config(parsedConfig)
	}

	return buildV2Config(parsedConfig)
}

func isV1(parsedConfig configDTO) bool {
	return parsedConfig.Version == 1 && parsedConfig.Module != ""
}

func buildV1Config(parsedConfig configDTO) (Config, error) {
	rules := make([]Rule, 0, len(parsedConfig.Rules))
	for _, ruleEntry := range parsedConfig.Rules {
		if err := validateRuleReferences(ruleEntry, parsedConfig.Layers); err != nil {
			return Config{}, err
		}
		rules = append(rules, Rule{
			Name:   ruleEntry.Name,
			Source: ruleEntry.Source,
			Deny:   append([]string{}, ruleEntry.Deny...),
		})
	}

	return Config{
		Layers:       copyLayers(parsedConfig.Layers),
		Rules:        rules,
		DetectCycles: parsedConfig.Generic.NoCyclicalDeps,
	}, nil
}

func buildV2Config(parsedConfig configDTO) (Config, error) {
	rules := make([]Rule, 0, len(parsedConfig.Rules))
	for _, ruleEntry := range parsedConfig.Rules {
		if err := validateRuleReferences(ruleEntry, parsedConfig.Layers); err != nil {
			return Config{}, err
		}
		rules = append(rules, Rule{
			Name:   ruleEntry.Name,
			Source: ruleEntry.Source,
			Deny:   append([]string{}, ruleEntry.Deny...),
		})
	}

	return Config{
		Layers:       copyLayers(parsedConfig.Layers),
		Rules:        rules,
		DetectCycles: parsedConfig.DetectCycles,
	}, nil
}

func validateRuleReferences(ruleEntry ruleDTO, layers map[string][]string) error {
	if _, ok := layers[ruleEntry.Source]; !ok {
		return fmt.Errorf("%w: rule %q references undefined source layer %q", ErrInvalidConfig, ruleEntry.Name, ruleEntry.Source)
	}
	for _, deny := range ruleEntry.Deny {
		if _, ok := layers[deny]; !ok {
			return fmt.Errorf("%w: rule %q references undefined deny layer %q", ErrInvalidConfig, ruleEntry.Name, deny)
		}
	}
	return nil
}

func copyLayers(layers map[string][]string) map[string][]string {
	result := make(map[string][]string, len(layers))
	for name, patterns := range layers {
		result[name] = append([]string{}, patterns...)
	}
	return result
}
