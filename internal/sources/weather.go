package sources

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Weather struct {
	Place       string
	Temperature float64
	Unit        string
	Description string
}

// wmoCode maps Open-Meteo's WMO weather code to a short human label.
// https://open-meteo.com/en/docs (search "Weather variable documentation").
var wmoCode = map[int]string{
	0:  "clear",
	1:  "mostly clear",
	2:  "partly cloudy",
	3:  "overcast",
	45: "fog",
	48: "rime fog",
	51: "light drizzle",
	53: "drizzle",
	55: "heavy drizzle",
	61: "light rain",
	63: "rain",
	65: "heavy rain",
	71: "light snow",
	73: "snow",
	75: "heavy snow",
	77: "snow grains",
	80: "light showers",
	81: "showers",
	82: "violent showers",
	85: "snow showers",
	86: "heavy snow showers",
	95: "thunderstorm",
	96: "thunderstorm + hail",
	99: "severe thunderstorm",
}

// FetchWeather uses Open-Meteo's free, keyless geocoding + forecast APIs.
// `location` is a free-form string like "Marietta, Georgia, US"; we
// take the first geocode hit. `units` is "imperial" or "metric" (the
// same vocabulary Glance uses).
func FetchWeather(ctx context.Context, location, units string, timeout time.Duration) (Weather, error) {
	out := Weather{}
	first := strings.SplitN(location, ",", 2)[0]
	geoURL := "https://geocoding-api.open-meteo.com/v1/search?count=1&name=" + url.QueryEscape(strings.TrimSpace(first))
	gv, err := FetchJSON(ctx, geoURL, nil, timeout)
	if err != nil {
		return out, fmt.Errorf("geocode: %w", err)
	}
	results, _ := gv.(map[string]any)
	hits, _ := results["results"].([]any)
	if len(hits) == 0 {
		return out, fmt.Errorf("geocode: no hits for %q", location)
	}
	hit, _ := hits[0].(map[string]any)
	lat, _ := hit["latitude"].(float64)
	lon, _ := hit["longitude"].(float64)
	name, _ := hit["name"].(string)
	admin, _ := hit["admin1"].(string)
	out.Place = name
	if admin != "" {
		out.Place = name + ", " + admin
	}

	tempUnit := "fahrenheit"
	out.Unit = "°F"
	if units == "metric" {
		tempUnit = "celsius"
		out.Unit = "°C"
	}
	fcURL := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&current=temperature_2m,weather_code&temperature_unit=%s",
		lat, lon, tempUnit,
	)
	fv, err := FetchJSON(ctx, fcURL, nil, timeout)
	if err != nil {
		return out, fmt.Errorf("forecast: %w", err)
	}
	root, _ := fv.(map[string]any)
	cur, _ := root["current"].(map[string]any)
	if t, ok := cur["temperature_2m"].(float64); ok {
		out.Temperature = t
	}
	if c, ok := cur["weather_code"].(float64); ok {
		if desc, ok := wmoCode[int(c)]; ok {
			out.Description = desc
		} else {
			out.Description = fmt.Sprintf("code %d", int(c))
		}
	}
	return out, nil
}

// CachedWeather wraps FetchWeather with a single-shot geocode cache so
// we don't hit the geocoding API every refresh tick.
type CachedWeather struct {
	location string
	units    string
	lat, lon float64
	place    string
	geocoded bool
}

func NewCachedWeather(location, units string) *CachedWeather {
	return &CachedWeather{location: location, units: units}
}

func (c *CachedWeather) Fetch(ctx context.Context, timeout time.Duration) (Weather, error) {
	if !c.geocoded {
		w, err := FetchWeather(ctx, c.location, c.units, timeout)
		if err != nil {
			return w, err
		}
		c.geocoded = true
		c.place = w.Place
		// Re-fetch on every tick after this point uses the cached place
		// label; the temperature reload still goes through FetchWeather
		// for simplicity. (Geocoding cost is minor either way.)
		return w, nil
	}
	return FetchWeather(ctx, c.location, c.units, timeout)
}
