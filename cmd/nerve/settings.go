package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

// secretKeys are settings that hold secrets or run credential helpers. A
// Brain may be public, so check refuses them there; they belong in a local
// overlay.
var secretKeys = []string{"apiKeyHelper", "awsAuthRefresh", "awsCredentialExport", "env", "otelHeadersHelper"}

// brainPlaceholder in any settings string becomes the Brain's checkout path.
const brainPlaceholder = "{{brain}}"

// readObject reads a JSON object, or returns nil when the file is missing.
func readObject(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if obj == nil {
		obj = map[string]any{}
	}
	return obj, nil
}

// overlayPath is the local overlay of a profile, or of configs that follow
// none.
func overlayPath(profile string) (string, error) {
	dir, err := machineDir()
	if profile == "" {
		profile = "default"
	}
	return filepath.Join(dir, "overlays", profile+".json"), err
}

// wantSettings merges the Brain's base, the config's profile and the local
// overlay. It returns nil when none of them exists.
func wantSettings(brain string, c Config) (map[string]any, error) {
	paths := []string{filepath.Join(brain, "settings", "base.json")}
	if c.Profile != "" {
		paths = append(paths, filepath.Join(brain, "settings", c.Profile+".json"))
	}
	overlay, err := overlayPath(c.Profile)
	if err != nil {
		return nil, err
	}
	var want map[string]any
	for _, p := range append(paths, overlay) {
		layer, err := readObject(p)
		if err != nil {
			return nil, err
		}
		if layer != nil {
			want = merge(want, layer)
		}
	}
	if want == nil {
		return nil, nil
	}
	return fillBrain(want, brain).(map[string]any), nil
}

// merge lays src over dst: objects merge key by key, arrays are joined
// without repeats, anything else in src wins.
func merge(dst, src map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		switch sv := v.(type) {
		case map[string]any:
			if dv, ok := out[k].(map[string]any); ok {
				out[k] = merge(dv, sv)
				continue
			}
		case []any:
			if dv, ok := out[k].([]any); ok {
				joined := slices.Clone(dv)
				for _, item := range sv {
					if !slices.ContainsFunc(joined, func(x any) bool { return reflect.DeepEqual(x, item) }) {
						joined = append(joined, item)
					}
				}
				out[k] = joined
				continue
			}
		}
		out[k] = v
	}
	return out
}

func fillBrain(v any, brain string) any {
	switch t := v.(type) {
	case string:
		return strings.ReplaceAll(t, brainPlaceholder, brain)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = fillBrain(x, brain)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			out[k] = fillBrain(x, brain)
		}
		return out
	}
	return v
}

// leaves flattens an object to dotted paths of its non-object values.
func leaves(obj map[string]any, prefix string, out map[string]any) map[string]any {
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range obj {
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			leaves(sub, prefix+k+".", out)
			continue
		}
		out[prefix+k] = v
	}
	return out
}

// syncSettings writes the config's settings.json from the Brain, unless
// Claude Code changed a key since the last write and the Brain does not
// agree: that drift is reported and the file left alone, or overwritten when
// discard is set. It returns whether drift held the file back.
func syncSettings(brain string, c Config, st *state, discard bool, out io.Writer) (bool, error) {
	want, err := wantSettings(brain, c)
	if err != nil {
		return false, err
	}
	if want == nil {
		if st.Settings == nil {
			return false, nil
		}
		want = map[string]any{}
	}
	path := filepath.Join(c.Dir, "settings.json")
	current, err := readObject(path)
	if err != nil {
		return false, err
	}
	have, last, goal := leaves(current, "", nil), leaves(st.Settings, "", nil), leaves(want, "", nil)
	var drift []string
	for _, k := range unionKeys(have, last) {
		if reflect.DeepEqual(have[k], last[k]) || reflect.DeepEqual(have[k], goal[k]) {
			continue
		}
		drift = append(drift, fmt.Sprintf("  local    %s: %s (Brain: %s)", k, show(have, k), show(goal, k)))
	}
	if len(drift) > 0 && !discard {
		fmt.Fprintf(out, "drift    %s: changed outside the Brain, not written (sync --discard overwrites):\n%s\n", path, strings.Join(drift, "\n"))
		return true, nil
	}
	if current == nil || !reflect.DeepEqual(have, goal) {
		b, _ := json.MarshalIndent(want, "", "  ")
		if err := os.MkdirAll(c.Dir, 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			return false, err
		}
		fmt.Fprintf(out, "wrote    %s\n", path)
	}
	st.Settings = want
	return false, nil
}

func unionKeys(a, b map[string]any) []string {
	var keys []string
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func show(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok {
		return "unset"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// settingsProblems lists what is wrong with a Brain's settings files.
func settingsProblems(root string, m Manifest) []string {
	files, _ := filepath.Glob(filepath.Join(root, "settings", "*.json"))
	var p []string
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		rel := filepath.Join("settings", filepath.Base(f))
		if name != "base" && !slices.Contains(m.Profiles, name) {
			p = append(p, fmt.Sprintf("%s: %q is neither base nor a profile in %s", rel, name, manifestFile))
		}
		obj, err := readObject(f)
		if err != nil {
			p = append(p, err.Error())
			continue
		}
		for _, k := range secretKeys {
			if _, ok := obj[k]; ok {
				p = append(p, fmt.Sprintf("%s: %s may hold secrets: move it to a local overlay", rel, k))
			}
		}
	}
	return p
}
