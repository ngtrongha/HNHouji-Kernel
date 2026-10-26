package experience

func schemaObject(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

// Constrain generation to the same snapshot used to resolve and validate actions.
func proposalSchema(legend []HabitApp, c Config, knobs []KernelKnob, ready, automatic bool) map[string]any {
	reason := map[string]any{"type": "string", "minLength": 1, "maxLength": 300}
	variants := []any{}
	for _, app := range legend {
		if automatic && c.Apps[app.Package] != "" && c.AIProfiles[app.Package] != c.Apps[app.Package] {
			continue
		}
		profiles := []string{"automatic", "daily", "windows-game", "linux"}
		if app.GameSupported {
			profiles = append(profiles, "native-game")
		}
		variants = append(variants, schemaObject(map[string]any{
			"kind":    map[string]any{"type": "string", "enum": []string{"app_profile"}},
			"app_id":  map[string]any{"type": "string", "enum": []string{app.ID}},
			"profile": map[string]any{"type": "string", "enum": profiles}, "reason": reason,
		}, "kind", "app_id", "profile", "reason"))
	}
	supported := false
	for _, knob := range knobs {
		if automatic && (!c.KernelTuning || !kernelControlEnabled(c, knob.Control)) {
			continue
		}
		levels := []string{"stock"}
		if ready && knob.Current != "" {
			levels = knob.Levels
		}
		if len(levels) == 0 {
			levels = []string{"stock"}
		}
		variants = append(variants, schemaObject(map[string]any{
			"kind":    map[string]any{"type": "string", "enum": []string{"kernel_level"}},
			"control": map[string]any{"type": "string", "enum": []string{knob.Control}},
			"level":   map[string]any{"type": "string", "enum": levels}, "reason": reason,
		}, "kind", "control", "level", "reason"))
		values := []bool{false}
		if ready && knob.State == "available" {
			values = append(values, true)
			supported = true
		}
		variants = append(variants, schemaObject(map[string]any{
			"kind":    map[string]any{"type": "string", "enum": []string{"kernel_control"}},
			"control": map[string]any{"type": "string", "enum": []string{knob.Control}},
			"enabled": map[string]any{"type": "boolean", "enum": values}, "reason": reason,
		}, "kind", "control", "enabled", "reason"))
	}
	values := []bool{false}
	if supported && (!automatic || c.KernelTuning) {
		values = append(values, true)
	}
	variants = append(variants, schemaObject(map[string]any{
		"kind":    map[string]any{"type": "string", "enum": []string{"kernel_preset"}},
		"enabled": map[string]any{"type": "boolean", "enum": values}, "reason": reason,
	}, "kind", "enabled", "reason"))
	return schemaObject(map[string]any{
		"answer":  map[string]any{"type": "string", "minLength": 1},
		"actions": map[string]any{"type": "array", "maxItems": 18, "items": map[string]any{"anyOf": variants}},
	}, "answer", "actions")
}
