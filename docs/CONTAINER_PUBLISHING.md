# Container Image Publishing

This project publishes container images to GitHub Container Registry (ghcr.io).

## GitHub Actions Workflow

A manual workflow is available to build and push container images: **Build and Push Container Image**

### Triggering the Workflow

1. Go to the **Actions** tab on GitHub
2. Select **Build and Push Container Image**
3. Click **Run workflow**
4. Configure optional inputs:
   - **Container image tag**: Custom tag (defaults to git short SHA if not provided)
   - **Push to registry**: Choose whether to push the built image (default: true)

### Automatic Image Tags

When pushed to ghcr.io, images are tagged with:
- Git short SHA (e.g., `a1b2c3d`)
- Semantic versions if using git tags (e.g., `v1.0.0`, `1.0`, `1`)
- Branch references for non-main branches

### Example Usage

**Pull the latest image:**
```bash
docker pull ghcr.io/echotools/nevr-agent:latest
```

**Pull a specific version:**
```bash
docker pull ghcr.io/echotools/nevr-agent:v1.0.0
```

**Pull the image by git SHA:**
```bash
docker pull ghcr.io/echotools/nevr-agent:a1b2c3d
```

## Using Pre-built Images with docker-compose

The `docker-compose.yml` supports using pre-built images from ghcr.io:

**Option 1: Build locally (default)**
```bash
docker-compose up
```

**Option 2: Use pre-built image**
```bash
SESSION_DATA_API_IMAGE=ghcr.io/echotools/nevr-agent:latest docker-compose up
```

**Option 3: Use specific version**
```bash
SESSION_DATA_API_IMAGE=ghcr.io/echotools/nevr-agent:v1.0.0 docker-compose up
```

**Option 4: Configure in .env file**
```bash
# In .env file:
SESSION_DATA_API_IMAGE=ghcr.io/echotools/nevr-agent:latest
```

## Authentication

To pull private images, log in to ghcr.io first:
```bash
echo ${{ secrets.GITHUB_TOKEN }} | docker login ghcr.io -u ${{ github.actor }} --password-stdin
```

For GitHub Actions, authentication is automatic with the `GITHUB_TOKEN` secret.

## Image Registry

Images are stored in GitHub Container Registry (ghcr.io):
- **Registry**: https://ghcr.io
- **Repository**: ghcr.io/echotools/nevr-agent
- **Visibility**: Inherited from repository settings

To view published images:
1. Go to the GitHub repository
2. Click on **Packages** on the right sidebar
3. Select **ghcr.io/echotools/nevr-agent**
