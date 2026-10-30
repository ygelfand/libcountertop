package wake

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zserge/microwakeword"

	"github.com/ygelfand/libcountertop/pkg/inference/oww"
	"github.com/ygelfand/libcountertop/pkg/inference/tflite"
)

// The reference satellite ships with this one selected.
const DefaultModel = "okay_nabu"

const DefaultThreshold = 0.85

const (
	WindowSize  = 5
	FeatureStep = 10
)

type Kind string

const (
	KindOpenWakeWord  Kind = "openwakeword"
	KindMicroWakeWord Kind = "microwakeword"
	KindPryon         Kind = "pryon"
	KindGemini        Kind = "gemini"
)

type Model struct {
	ID string

	Phrase string

	// BCP-47 tags.
	Languages []string

	Kind Kind

	Path   string
	Config microwakeword.Config

	Keyword string
}

type manifest struct {
	WakeWord         string   `json:"wake_word"`
	Model            string   `json:"model"`
	TrainedLanguages []string `json:"trained_languages"`

	Micro struct {
		SlidingWindowSize int `json:"sliding_window_size"`
		FeatureStepSize   int `json:"feature_step_size"`
	} `json:"micro"`
}

func Installed(dir string) ([]Model, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []Model
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tflite" {
			continue
		}

		id := strings.TrimSuffix(e.Name(), ".tflite")
		out = append(out, load(dir, id))
	}
	return out, nil
}

// An openWakeWord classifier's input is a window of embeddings, [1, frames, 96].
func kindOf(path string) Kind {
	raw, err := os.ReadFile(path)
	if err != nil {
		return KindMicroWakeWord
	}
	m, err := tflite.Parse(raw)
	if err != nil {
		return KindMicroWakeWord
	}

	if len(m.Subgraphs) == 0 {
		return KindMicroWakeWord
	}
	g := m.Subgraphs[0]
	if len(g.Inputs) == 0 {
		return KindMicroWakeWord
	}
	shape := g.Tensors[g.Inputs[0]].Shape
	if len(shape) == 3 && shape[2] == oww.EmbeddingDims {
		return KindOpenWakeWord
	}
	return KindMicroWakeWord
}

func load(dir, id string) Model {
	path := filepath.Join(dir, id+".tflite")
	m := Model{
		ID:        id,
		Phrase:    phrase(id),
		Languages: []string{"en"},
		Kind:      kindOf(path),
		Path:      path,
		Config: microwakeword.Config{
			SlidingWindowSize: WindowSize,
			FeaturesStepMs:    FeatureStep,
		},
	}
	m.Config.ModelPath = m.Path

	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return m
	}

	var man manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return m
	}

	if man.WakeWord != "" {
		m.Phrase = man.WakeWord
	}
	if len(man.TrainedLanguages) > 0 {
		m.Languages = man.TrainedLanguages
	}
	if man.Model != "" && filepath.IsLocal(man.Model) {
		m.Path = filepath.Join(dir, man.Model)
		m.Config.ModelPath = m.Path
	}

	if man.Micro.SlidingWindowSize > 0 {
		m.Config.SlidingWindowSize = man.Micro.SlidingWindowSize
	}
	if man.Micro.FeatureStepSize > 0 {
		m.Config.FeaturesStepMs = man.Micro.FeatureStepSize
	}
	return m
}

func phrase(id string) string {
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '_' || r == '-' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func Pick(models []Model, id string) Model {
	for _, m := range models {
		if m.ID == id {
			return m
		}
	}
	if len(models) == 0 {
		return Model{}
	}
	return models[0]
}

func Find(models []Model, id string) (Model, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

func OfKind(models []Model, k Kind) []Model {
	var out []Model
	for _, m := range models {
		if m.Kind == k {
			out = append(out, m)
		}
	}
	return out
}
