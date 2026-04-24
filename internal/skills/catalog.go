package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"osagentmvp/internal/models"
)

type Catalog struct {
	items []models.SkillDefinition
}

func Empty() *Catalog {
	return &Catalog{}
}

func Load(dir string) (*Catalog, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("glob skills: %w", err)
	}
	sort.Strings(paths)

	items := make([]models.SkillDefinition, 0, len(paths))
	for _, path := range paths {
		bytes, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var item models.SkillDefinition
		if err := json.Unmarshal(bytes, &item); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		items = append(items, item)
	}
	return &Catalog{items: items}, nil
}

func (c *Catalog) Select(input string, limit int) []models.SkillSummary {
	if limit <= 0 {
		limit = 4
	}
	type scored struct {
		score int
		item  models.SkillDefinition
	}
	inputLower := strings.ToLower(input)
	var scoredItems []scored
	for _, item := range c.items {
		score := 0
		for _, sample := range append(item.IntentExamples, item.DecisionHints...) {
			if strings.Contains(inputLower, strings.ToLower(sample)) {
				score += 2
			}
		}
		for _, token := range strings.Fields(strings.ToLower(item.Title + " " + item.Description)) {
			if len(token) > 2 && strings.Contains(inputLower, token) {
				score++
			}
		}
		scoredItems = append(scoredItems, scored{score: score, item: item})
	}
	sort.SliceStable(scoredItems, func(i, j int) bool {
		return scoredItems[i].score > scoredItems[j].score
	})

	result := make([]models.SkillSummary, 0, limit)
	for _, entry := range scoredItems {
		if len(result) >= limit {
			break
		}
		if entry.score == 0 && len(result) > 0 {
			continue
		}
		result = append(result, models.SkillSummary{
			ID:           entry.item.ID,
			Title:        entry.item.Title,
			Description:  entry.item.Description,
			RiskCategory: entry.item.RiskCategory,
			Examples:     entry.item.IntentExamples,
		})
	}
	return result
}
