# Quick Start: ghcr.io Container Images

## For Users - Pull and Run

### Pull the latest image
```bash
docker pull ghcr.io/echotools/nevr-agent:latest
```

### Run with docker-compose (uses local build by default)
```bash
docker-compose up
```

### Run with pre-built image from ghcr.io
```bash
SESSION_DATA_API_IMAGE=ghcr.io/echotools/nevr-agent:latest docker-compose up
```

---

## For Maintainers - Build and Push New Images

### Method 1: Manual Trigger (Recommended)
1. Go to GitHub repo → **Actions** tab
2. Select **Build and Push Container Image**
3. Click **Run workflow**
4. Leave tag empty (uses git short SHA) or specify custom tag
5. Ensure "Push to registry" is checked
6. Click **Run workflow** again
7. Wait for build to complete (~5 mins)

**Result**: Image pushed to `ghcr.io/echotools/nevr-agent:TAG`

### Method 2: Manual Build & Push (Local)
```bash
# Build image locally
docker build -t ghcr.io/echotools/nevr-agent:v1.0.0 .

# Log in to ghcr.io
echo $GITHUB_TOKEN | docker login ghcr.io -u YOUR_USERNAME --password-stdin

# Push image
docker push ghcr.io/echotools/nevr-agent:v1.0.0
```

---

## Image Tags and Versions

| Tag | Use Case | Example |
|-----|----------|---------|
| `latest` | Latest stable image | `ghcr.io/echotools/nevr-agent:latest` |
| `v1.0.0` | Semantic version tag | `ghcr.io/echotools/nevr-agent:v1.0.0` |
| `a1b2c3d` | Git commit SHA | `ghcr.io/echotools/nevr-agent:a1b2c3d` |
| `main` | Default branch | `ghcr.io/echotools/nevr-agent:main` |

---

## View Published Images

1. Go to GitHub repo → **Packages** (right sidebar)
2. Click **ghcr.io/echotools/nevr-agent**
3. See all published versions with creation dates

---

## Troubleshooting

### "Failed to pull image"
- Ensure you're logged in: `docker login ghcr.io`
- Check image exists in Packages section
- Verify tag name is correct

### "Push failed: unauthorized"
- Verify `GITHUB_TOKEN` has `write:packages` permission
- In GitHub Actions, `GITHUB_TOKEN` is automatic
- For local push, regenerate PAT with `write:packages` scope

### "Image not found" after workflow completes
- Wait 30 seconds for registry to sync
- Refresh the Packages page
- Check workflow logs for final image reference

---

## Documentation

- [CONTAINER_PUBLISHING.md](CONTAINER_PUBLISHING.md) - Detailed publishing guide
- [GHCR_SETUP_SUMMARY.md](GHCR_SETUP_SUMMARY.md) - Complete setup summary
- [README.md](README.md) - General project documentation
- [Dockerfile](Dockerfile) - Container build definition

---

## Next: Automate on Release (Optional)

To automatically push images when creating GitHub releases:

Edit `.github/workflows/build-and-push.yml` and add:
```yaml
on:
  workflow_dispatch:
    ...
  release:
    types: [published]
```

This will automatically build and push whenever a release is published.
