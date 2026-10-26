package experience

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type settingTarget struct{ Namespace, Key, Field, Value string }
type systemDefinition struct {
	ID, Label string
	Choices   []string
	Targets   func(string) []settingTarget
}
type SystemControl struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Choice  string   `json:"choice"`
	Choices []string `json:"choices"`
	State   string   `json:"state"`
	Current []string `json:"current"`
}

func targets(namespace string, keys []string, value string) []settingTarget {
	result := []settingTarget{}
	for _, key := range keys {
		result = append(result, settingTarget{namespace, key, "", value})
	}
	return result
}

var systemDefinitions = []systemDefinition{
	{"wifi_scanning", "Quét Wi-Fi nền", []string{"stock", "off"}, func(string) []settingTarget { return targets("global", []string{"wifi_scan_always_enabled"}, "0") }},
	{"bluetooth_scanning", "Quét Bluetooth nền", []string{"stock", "off"}, func(string) []settingTarget { return targets("global", []string{"ble_scan_always_enabled"}, "0") }},
	{"background_location", "Cập nhật vị trí nền", []string{"stock", "1h", "2h", "4h"}, func(c string) []settingTarget {
		v := map[string]string{"1h": "3600000", "2h": "7200000", "4h": "14400000"}[c]
		return targets("global", []string{"location_background_throttle_interval_ms", "location_background_throttle_proximity_alert_interval_ms"}, v)
	}},
	{"saver_trigger", "Ngưỡng Battery Saver", []string{"stock", "15", "20", "30"}, func(c string) []settingTarget {
		return []settingTarget{{"global", "automatic_power_save_mode", "", "0"}, {"global", "low_power_trigger_level", "", c}}
	}},
	{"saver_dim", "Độ sáng khi Battery Saver", []string{"stock", "90", "80", "70", "60"}, func(c string) []settingTarget {
		return []settingTarget{{"global", "battery_saver_constants", "enable_brightness_adjustment", "true"}, {"global", "battery_saver_constants", "adjust_brightness_disabled", "false"}, {"global", "battery_saver_constants", "adjust_brightness_factor", map[string]string{"90": "0.9", "80": "0.8", "70": "0.7", "60": "0.6"}[c]}}
	}},
	{"blur", "Blur giao diện", []string{"stock", "off"}, func(string) []settingTarget { return targets("global", []string{"disable_window_blurs"}, "1") }},
	{"animation", "Tốc độ animation", []string{"stock", "0.5", "1"}, func(c string) []settingTarget {
		return targets("global", []string{"window_animation_scale", "transition_animation_scale", "animator_duration_scale"}, c)
	}},
	{"peak_refresh", "Giới hạn tần số quét", []string{"stock", "60", "90", "120"}, func(c string) []settingTarget { return targets("system", []string{"peak_refresh_rate"}, c) }},
}

func systemDefinitionFor(id string) (systemDefinition, bool) {
	for _, d := range systemDefinitions {
		if d.ID == id {
			return d, true
		}
	}
	return systemDefinition{}, false
}
func validSystemChoice(id, choice string) bool {
	d, ok := systemDefinitionFor(id)
	if !ok {
		return false
	}
	for _, c := range d.Choices {
		if c == choice {
			return true
		}
	}
	return false
}
func systemEntrySpec(id string) (settingTarget, error) {
	for _, d := range systemDefinitions {
		for i, t := range d.Targets(d.Choices[1]) {
			if id == fmt.Sprintf("%s:%d", d.ID, i) {
				return t, nil
			}
		}
	}
	return settingTarget{}, errors.New("unknown system entry")
}
func validSettingValue(v string) bool { return len(v) <= 4096 && !strings.ContainsAny(v, "\r\n\x00") }
func validSystemValue(t settingTarget, v string) bool {
	if v == "null" {
		return true
	}
	if !validSettingValue(v) {
		return false
	}
	if t.Field == "enable_brightness_adjustment" || t.Field == "adjust_brightness_disabled" {
		return v == "true" || v == "false"
	}
	n, err := strconv.ParseFloat(v, 64)
	return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0
}
func settingFields(raw string) (map[string]string, error) {
	result := map[string]string{}
	if !validSettingValue(raw) {
		return nil, errors.New("invalid compound setting")
	}
	if raw == "null" || raw == "" {
		return result, nil
	}
	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" || !validSettingValue(v) {
			return nil, errors.New("invalid compound setting")
		}
		if _, exists := result[k]; exists {
			return nil, errors.New("duplicate setting field")
		}
		result[k] = v
	}
	return result, nil
}
func (a *App) currentSystem(ctx context.Context, id string) (string, error) {
	t, err := systemEntrySpec(id)
	if err != nil {
		return "", err
	}
	raw, err := a.run(ctx, "settings", "--user", "0", "get", t.Namespace, t.Key)
	if err != nil || !validSettingValue(raw) || raw == "" || strings.Contains(raw, "Exception") {
		return "", errors.New("system setting unreadable")
	}
	return systemSettingValue(t, raw)
}
func systemSettingValue(t settingTarget, raw string) (string, error) {
	if t.Field == "" {
		if !validSystemValue(t, raw) {
			return "", errors.New("system setting invalid")
		}
		return raw, nil
	}
	fields, err := settingFields(raw)
	if err != nil {
		return "", err
	}
	if v, ok := fields[t.Field]; ok {
		if !validSystemValue(t, v) {
			return "", errors.New("system field invalid")
		}
		return v, nil
	}
	return "null", nil
}
func (a *App) writeSystem(ctx context.Context, id, value string) error {
	t, err := systemEntrySpec(id)
	if err != nil {
		return err
	}
	if !validSystemValue(t, value) {
		return errors.New("invalid setting value")
	}
	if t.Field != "" {
		raw, e := a.run(ctx, "settings", "--user", "0", "get", t.Namespace, t.Key)
		if e != nil {
			return e
		}
		fields, e := settingFields(raw)
		if e != nil {
			return e
		}
		if value == "null" {
			delete(fields, t.Field)
		} else {
			fields[t.Field] = value
		}
		keys := []string{}
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := []string{}
		for _, k := range keys {
			pairs = append(pairs, k+"="+fields[k])
		}
		value = strings.Join(pairs, ",")
		if value == "" {
			value = "null"
		}
	}
	args := []string{"--user", "0", "put", t.Namespace, t.Key, value}
	if value == "null" {
		args = []string{"--user", "0", "delete", t.Namespace, t.Key}
	}
	out, err := a.run(ctx, "settings", args...)
	if err != nil || strings.Contains(out, "Error") || strings.Contains(out, "Exception") {
		return errors.New("Android rejected system setting")
	}
	return nil
}

var displayRateRE = regexp.MustCompile(`(?:fps|refreshRate|vsyncRate)\s*[=:]\s*([0-9]+(?:\.[0-9]+)?)`)
var primaryDisplayRE = regexp.MustCompile(`displayId[ =]+0\b`)

func primaryDisplayModes(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, "mBaseDisplayInfo=DisplayInfo{") && primaryDisplayRE.MatchString(line) {
			_, modes, ok := strings.Cut(line, "supportedModes")
			if ok {
				modes, _, _ = strings.Cut(modes, "]")
				return modes
			}
		}
	}
	return ""
}

func (a *App) systemSettingsStatus(ctx context.Context, c Config) []SystemControl {
	result := []SystemControl{}
	j, _ := a.journal()
	values := map[string]string{}
	wanted := map[string]bool{"system:min_refresh_rate": true}
	for _, definition := range systemDefinitions {
		for _, target := range definition.Targets(definition.Choices[1]) {
			wanted[target.Namespace+":"+target.Key] = true
		}
	}
	for _, namespace := range []string{"global", "system"} {
		raw, err := a.run(ctx, "settings", "--user", "0", "list", namespace)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(raw, "\n") {
			key, value, ok := strings.Cut(line, "=")
			if ok && wanted[namespace+":"+key] {
				values[namespace+":"+key] = value
			}
		}
	}
	for _, d := range systemDefinitions {
		choice := c.SystemSettings[d.ID]
		if choice == "" {
			choice = "stock"
		}
		control := SystemControl{ID: d.ID, Label: d.Label, Choice: choice, Choices: []string{"stock"}, State: "unsupported", Current: []string{}}
		ok := true
		for _, t := range d.Targets(d.Choices[1]) {
			raw, found := values[t.Namespace+":"+t.Key]
			v, err := systemSettingValue(t, raw)
			control.Current = append(control.Current, v)
			if !found || err != nil || (t.Field == "" && v == "null") {
				ok = false
			}
			if t.Field != "" {
				if raw == "null" || raw == "" {
					ok = false
				}
			}
		}
		if ok {
			control.State = "available"
			control.Choices = append([]string{}, d.Choices...)
			if d.ID == "peak_refresh" {
				control.Choices = []string{"stock"}
				display, err := a.run(ctx, "dumpsys", "display")
				minimum, exists := values["system:min_refresh_rate"]
				min, nerr := strconv.ParseFloat(minimum, 64)
				if err == nil && exists && nerr == nil && !math.IsNaN(min) && !math.IsInf(min, 0) && min >= 0 {
					for _, candidate := range d.Choices[1:] {
						want, _ := strconv.ParseFloat(candidate, 64)
						if want < min {
							continue
						}
						for _, match := range displayRateRE.FindAllStringSubmatch(primaryDisplayModes(display), -1) {
							rate, _ := strconv.ParseFloat(match[1], 64)
							if math.Abs(rate-want) < 0.2 {
								control.Choices = append(control.Choices, candidate)
								break
							}
						}
					}
				}
				if len(control.Choices) == 1 {
					control.State = "unsupported"
				}
			}
		}
		if control.State == "available" && choice != "stock" {
			control.State = "pending"
			match := true
			for i, t := range d.Targets(choice) {
				if control.Current[i] != t.Value {
					match = false
				}
			}
			if match {
				control.State = "already-set"
			}
			for _, entry := range j.Entries {
				if entry.Kind == "system" && strings.HasPrefix(entry.Package, d.ID+":") {
					v, e := a.currentSystem(ctx, entry.Package)
					if e != nil || v != entry.Applied || entry.Conflict {
						control.State = "conflict"
						break
					}
					if match {
						control.State = "applied"
					}
				}
			}
		}
		result = append(result, control)
	}
	return result
}
func (a *App) validateSystemChoices(ctx context.Context, c Config) error {
	for _, control := range a.systemSettingsStatus(ctx, c) {
		if control.Choice == "stock" {
			continue
		}
		found := false
		for _, v := range control.Choices {
			if v == control.Choice {
				found = true
			}
		}
		if !found {
			return errors.New("system setting unsupported: " + control.ID)
		}
	}
	return nil
}
func (a *App) tuneSystem(ctx context.Context, c Config, j *Journal) error {
	owned := false
	for _, entry := range j.Entries {
		if entry.Kind == "system" {
			owned = true
			v, err := a.current(ctx, entry)
			if err != nil {
				return err
			}
			if entry.Conflict || v != entry.Applied {
				return errors.New("system setting changed externally; preserved")
			}
		}
	}
	if len(c.SystemSettings) == 0 && !owned {
		return nil
	}
	wanted := map[string]string{}
	for _, control := range a.systemSettingsStatus(ctx, c) {
		if control.Choice == "stock" {
			continue
		}
		supported := false
		for _, choice := range control.Choices {
			supported = supported || choice == control.Choice
		}
		if !supported {
			continue
		}
		d, _ := systemDefinitionFor(control.ID)
		for i, t := range d.Targets(control.Choice) {
			wanted[fmt.Sprintf("%s:%d", d.ID, i)] = t.Value
		}
	}
	if err := a.restoreEntries(ctx, j, func(e Entry) bool { return e.Kind == "system" && wanted[e.Package] != e.Applied }); err != nil {
		return err
	}
	for _, d := range systemDefinitions {
		for i := range d.Targets(d.Choices[1]) {
			id := fmt.Sprintf("%s:%d", d.ID, i)
			if v, ok := wanted[id]; ok {
				if err := a.apply(ctx, j, Entry{Kind: "system", Package: id, Applied: v}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
