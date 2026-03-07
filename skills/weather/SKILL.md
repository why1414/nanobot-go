---
name: weather
description: Get current weather information for any location.
---

# Weather

Use the `exec` tool to check weather information.

## Quick Check

Get current weather using curl and wttr.in:

```bash
curl -s "wttr.in/Beijing?format=3"
```

Output: `Beijing: ☀️ +12°C`

## Detailed Weather

Get full weather report:

```bash
curl -s "wttr.in/Shanghai"
```

This returns a detailed ASCII-art weather forecast.

## Supported Formats

| Format | Example |
|--------|---------|
| Current only | `wttr.in/Beijing?format=3` |
| Short | `wttr.in/Beijing?format="%t+%w"` |
| JSON | `wttr.in/Beijing?format=j1` |

## Common Locations

- Beijing: `wttr.in/Beijing`
- Shanghai: `wttr.in/Shanghai`
- New York: `wttr.in/New_York`
- London: `wttr.in/London`
- Tokyo: `wttr.in/Tokyo`

## Examples

User asks: "What's the weather in Beijing?"

Use exec tool:
```bash
curl -s "wttr.in/Beijing?format=3"
```

User asks: "Will it rain tomorrow in Shanghai?"

Use exec tool:
```bash
curl -s "wttr.in/Shanghai" | grep -A 3 "Tomorrow"
```

## Notes

- No API key required
- wttr.in is a free weather service
- Supports location names, airports codes, and coordinates
- Works internationally
