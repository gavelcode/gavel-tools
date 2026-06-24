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
	var dto configDTO
	if err := yaml.Unmarshal(data, &dto); err != nil {
		return Config{}, fmt.Errorf("parse YAML: %w", err)
	}

	if len(dto.Layers) == 0 {
		return Config{}, fmt.Errorf("%w: at least one layer is required", ErrInvalidConfig)
	}

	if isV1(dto) {
		return buildV1Config(dto)
	}

	return buildV2Config(dto)
}

func isV1(dto configDTO) bool {
	return dto.Version == 1 && dto.Module != ""
}

func buildV1Config(dto configDTO) (Config, error) {
	rules := make([]Rule, 0, len(dto.Rules))
	for _, ruleDTO := range dto.Rules {
		if err := validateRuleReferences(ruleDTO, dto.Layers); err != nil {
			return Config{}, err
		}
		rules = append(rules, Rule{
			Name:   ruleDTO.Name,
			Source: ruleDTO.Source,
			Deny:   append([]string{}, ruleDTO.Deny...),
		})
	}

	return Config{
		Layers:       copyLayers(dto.Layers),
		Rules:        rules,
		DetectCycles: dto.Generic.NoCyclicalDeps,
	}, nil
}

func buildV2Config(dto configDTO) (Config, error) {
	rules := make([]Rule, 0, len(dto.Rules))
	for _, ruleDTO := range dto.Rules {
		if err := validateRuleReferences(ruleDTO, dto.Layers); err != nil {
			return Config{}, err
		}
		rules = append(rules, Rule{
			Name:   ruleDTO.Name,
			Source: ruleDTO.Source,
			Deny:   append([]string{}, ruleDTO.Deny...),
		})
	}

	return Config{
		Layers:       copyLayers(dto.Layers),
		Rules:        rules,
		DetectCycles: dto.DetectCycles,
	}, nil
}

func validateRuleReferences(rule ruleDTO, layers map[string][]string) error {
	if _, ok := layers[rule.Source]; !ok {
		return fmt.Errorf("%w: rule %q references undefined source layer %q", ErrInvalidConfig, rule.Name, rule.Source)
	}
	for _, deny := range rule.Deny {
		if _, ok := layers[deny]; !ok {
			return fmt.Errorf("%w: rule %q references undefined deny layer %q", ErrInvalidConfig, rule.Name, deny)
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
