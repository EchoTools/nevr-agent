# ghcr.io Integration Summary

This document summarizes all changes made to support publishing container images to GitHub Container Registry (ghcr.io).

## Files Created

### 1. `.github/workflows/build-and-push.yml`
- **Purpose**: GitHub Actions workflow for manual container image building and pushing
- **Features**:
  - Manual trigger via workflow_dispatch
  - Configurable image tag (defaults to git short SHA)
  - Optional push to registry
  - Multi-stage build with layer caching
  - Automatic Docker layer caching via GitHub Actions cache
  - Metadata extraction (versions, branches, SHA)
  - Secure authentication using GITHUB_TOKEN

- **How to trigger**:
  1. Go to Actions tab on GitHub
  2. Select "Build and Push Container Image"
  3. Click "Run workflow"
  4. Configure tag and push options
  5. Watch the workflow execute and push to ghcr.io

### 2. `CONTAINER_PUBLISHING.md`
- Comprehensive documentation for container image usage
- Instructions for:
  - Triggering the GitHub Actions workflow
  - Pulling images from ghcr.io
  - Using pre-built images with docker-compose
  - Authentication for private images
  - Viewing published images

## Files Modified

### 1. `Dockerfile`
- **Changes**: Added OCI (Open Container Initiative) labels
- **Added labels**:
  - `org.opencontainers.image.title`
  - `org.opencontainers.image.description`
  - `org.opencontainers.image.url`
  - `org.opencontainers.image.source`
  - `org.opencontainers.image.vendor`
- **Benefit**: Better metadata for container registries and improved discoverability

### 2. `docker-compose.yml`
- **Changes**:
  - Modified `session-data-api` service to support both local builds and pre-built images
  - Added `image` field with default ghcr.io reference: `${SESSION_DATA_API_IMAGE:-ghcr.io/echotools/nevr-agent:latest}`
  - Kept `build` context for local development
  - Priority: Uses `SESSION_DATA_API_IMAGE` env var if set, otherwise builds locally

- **Usage**:
  - `docker-compose up` - Builds locally (default)
  - `SESSION_DATA_API_IMAGE=ghcr.io/echotools/nevr-agent:v1.0.0 docker-compose up` - Uses specific pre-built version
  - Set in `.env` file for persistent configuration

### 3. `.env.compose`
- **Changes**: Added `SESSION_DATA_API_IMAGE` configuration option
- **Documentation**: Added examples showing:
  - How to use latest image
  - How to use specific version tags
  - Default behavior (local build)

### 4. `README.md`
- **Changes**: Added container image section to Installation
- **Added documentation**: Quick links to docker pull and docker-compose instructions
- **Reference**: Link to CONTAINER_PUBLISHING.md for detailed info

## Image Registry Details

### Registry Location
- **URL**: https://ghcr.io
- **Repository**: `ghcr.io/echotools/nevr-agent`
- **Visibility**: Inherited from GitHub repository settings

### Image Tags
When pushed, images are tagged with:
- **Git short SHA** (7 characters, e.g., `a1b2c3d`)
- **Semantic versions** if using git tags (e.g., `v1.0.0`, `1.0`, `1`)
- **Branch references** for non-main branches (e.g., `main`, `develop`)
- **`latest`** tag for the default image

### Example Image References
```bash
# Latest image
ghcr.io/echotools/nevr-agent:latest

# Specific version
ghcr.io/echotools/nevr-agent:v1.0.0

# Git commit SHA
ghcr.io/echotools/nevr-agent:a1b2c3d

# Branch
ghcr.io/echotools/nevr-agent:main
```

## Build Process Details

### GitHub Actions Workflow (`build-and-push.yml`)

**Trigger**: Manual dispatch via GitHub Actions UI

**Steps**:
1. Checkout repository
2. Set up Docker Buildx (multi-platform support)
3. Log in to ghcr.io (if pushing)
4. Determine image tag from inputs or git SHA
5. Extract metadata (versions, tags, labels)
6. Build and push image:
   - Multi-platform support ready (via Buildx)
   - Layer caching enabled (type=gha)
   - Image push conditional on input flag

**Inputs**:
- `tag`: Custom image tag (optional, defaults to git short SHA)
- `push`: Whether to push to registry (default: true)

**Output**: 
- Build artifacts stored in GitHub Actions cache
- Image pushed to ghcr.io/echotools/nevr-agent:TAG (if push=true)
- Workflow logs show final image reference

## Authentication

### GitHub Actions (Automatic)
The workflow uses `secrets.GITHUB_TOKEN` which is automatically provided by GitHub Actions for:
- Pushing to ghcr.io
- Accessing private repos (if applicable)

### Local Docker Usage
To pull private images locally:
```bash
echo $GITHUB_TOKEN | docker login ghcr.io -u YOUR_USERNAME --password-stdin
docker pull ghcr.io/echotools/nevr-agent:TAG
```

## Backwards Compatibility

### Local Development
- `docker-compose up` still builds locally by default
- No breaking changes to existing workflows
- Optional configuration to use pre-built images

### Binary Releases
- Separate from container images
- Continue to be published to GitHub Releases
- Can be used independently of containers

## Next Steps

1. **Commit and push** all changes to main branch
2. **Run workflow manually** to build first image:
   - Go to Actions → Build and Push Container Image
   - Click Run workflow
   - Leave tag empty (uses git SHA)
   - Wait for build to complete
3. **Verify image** on ghcr.io:
   - View in GitHub repo Packages section
   - Test with `docker pull`
4. **Update documentation** if deploying to production
5. **Set up automation** (optional):
   - Modify workflow to auto-trigger on release
   - Add scheduled builds for nightly images

## Rollback

To revert to Docker Hub or another registry:
1. Update `docker-compose.yml` session-data-api image reference
2. Modify `.env.compose` SESSION_DATA_API_IMAGE default
3. Update GitHub Actions workflow registry variable
4. Remove or archive CONTAINER_PUBLISHING.md

However, ghcr.io is recommended as it integrates seamlessly with GitHub and provides better security with GITHUB_TOKEN authentication.
