# Hugo shortcodes for photo library

Copy `photo.html` and `album.html` to your Hugo site's `layouts/shortcodes/` directory.

## Configuration

Add your photo server URL to your Hugo config:

```toml
# hugo.toml
[params]
photoServer = "https://photos.yourdomain.com"
```

```yaml
# hugo.yaml
params:
  photoServer: "https://photos.yourdomain.com"
```

## Usage

### Single photo

```
{{< photo id="06bq7xhnr03mlz6r" >}}

{{< photo id="06bq7xhnr03mlz6r" caption="Sunrise over Dawson Creek" >}}

{{< photo id="06bq7xhnr03mlz6r" size="thumb" >}}

{{< photo id="06bq7xhnr03mlz6r" link="false" >}}
```

Find the photo ID with:
```bash
photo search --location "Dawson Creek"
photo show <id>
```

### Album link

```
{{< album slug="france-2024" >}}

{{< album slug="france-2024" title="France 2024" >}}
```

Find the album slug with:
```bash
photo album list
```

## CSS

Add the contents of `shortcodes.css` to your theme's stylesheet,
adjusting variables to match your theme's design tokens.

## Notes

- Photos must be marked as published (`photo update --published <id>`) to
  be accessible without authentication.
- `photo publish` marks all uploaded photos as published automatically.
- RAW files are never published regardless of settings.
- URLs are stable — a photo's ID never changes after import.
