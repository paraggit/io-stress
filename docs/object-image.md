# Create the object worker image

The object workload does not run S3 calls on your laptop. Each phase runs in a pod, and that pod needs an image that contains this binary at `/usr/local/bin/odf-io-stress`. The pod starts it as:

```text
/usr/local/bin/odf-io-stress s3-worker --spec /etc/objstress/spec.json
```

The default image is `ghcr.io/paraggit/odf-io-stress:latest`. Build your own when that tag is missing, private, or older than the binary you are running. Pass the result with `--object-image`. The same image serves NooBaa and RGW object runs; only the cluster bucket endpoint and credentials differ.

The FIO image (`--image`, default `quay.io/ocsci/nginx:fio`) is a different container. It does not run the object workload.

## What you need

- This repository, checked out at the commit you want to run
- Docker or Podman
- A registry the cluster can pull without a pull secret in the test namespace

Every run deletes and recreates the namespace (`odf-io-stress` by default). A pull secret created inside that namespace is removed at the start of the next run. Publish the image so nodes can pull it with the namespace's default service account. A public package on GitHub Container Registry or Quay is the usual choice.

## Build

From the repository root:

```bash
make image IMAGE=quay.io/my-org/odf-io-stress:dev
```

`IMAGE` defaults to `quay.io/paraggit/odf-io-stress:<git describe>`. Set it to the name you will pass to `--object-image`.

The same build without Make:

```bash
docker build -f build/Dockerfile -t quay.io/my-org/odf-io-stress:dev .
```

Podman accepts the same arguments:

```bash
podman build -f build/Dockerfile -t quay.io/my-org/odf-io-stress:dev .
```

`build/Dockerfile` is a two-stage build. The first stage is `golang:1.26` and compiles `./cmd/odf-io-stress` with `CGO_ENABLED=0`. The second stage is `registry.access.redhat.com/ubi9/ubi-minimal` and copies the binary to `/usr/local/bin/odf-io-stress`. You do not need Go installed on the machine that builds the image.

The pod overrides the image entrypoint, runs as UID 1000, and drops all capabilities. Do not add a root-only entrypoint or a shell that the binary depends on.

## Check the image locally

```bash
docker run --rm quay.io/my-org/odf-io-stress:dev --help
```

`--help` must list `run`. The hidden `s3-worker` command is not in that help text. The cluster invokes it directly.

## Push

```bash
docker login quay.io
docker push quay.io/my-org/odf-io-stress:dev
```

Or:

```bash
make image-push IMAGE=quay.io/my-org/odf-io-stress:dev
```

`make image-push` builds first, then pushes. The cluster must be able to resolve and pull that exact reference.

## GitHub Container Registry

Each push to `main` publishes the worker image with `GITHUB_TOKEN`:

```text
ghcr.io/paraggit/odf-io-stress:v0.0.<run>-<sha>
ghcr.io/paraggit/odf-io-stress:latest
```

A fork publishes under its own owner, `ghcr.io/<owner>/odf-io-stress`. The workflow also pushes `quay.io/paraggit/odf-io-stress` when `QUAY_USERNAME` and `QUAY_PASSWORD` are set. The tool default is `ghcr.io/paraggit/odf-io-stress:latest`.

After the first push, open the package settings and set the visibility to Public if the cluster should pull it with no credentials. A private package returns `401` or `403` to the worker pod, and this tool does not install a pull secret.

To publish a tag yourself, create a personal access token with `write:packages` and `read:packages`, then:

```bash
echo "$GHCR_TOKEN" | docker login ghcr.io -u YOUR_GITHUB_USER --password-stdin
make image-push IMAGE=ghcr.io/paraggit/odf-io-stress:dev
```

`build/Dockerfile` sets `org.opencontainers.image.source` so GitHub links the package to this repository. The workflow passes the repository URL as `SOURCE_URL`. A local build uses `https://github.com/paraggit/io-stress`.

Worker pods do not set `imagePullPolicy`. Kubernetes then uses `Always` when the tag is `latest`, and `IfNotPresent` for any other tag. Rebuilds of a tag such as `dev` stay cached on the node until you use a new tag.

## Run with that image

```bash
# Default image, ghcr.io/paraggit/odf-io-stress:latest
./odf-io-stress run --object-only --object-profile smoke \
  --results-dir results/object-smoke

# Image you built and pushed
./odf-io-stress run --object-only --object-profile smoke \
  --object-image quay.io/my-org/odf-io-stress:dev \
  --results-dir results/object-smoke
```

`--object-image` overrides the profile default and `tools.object.image` in a config file. The same flag works with `--object` when FIO runs in the same namespace.

Confirm the reference before creating anything:

```bash
./odf-io-stress run --object-only --object-profile smoke --dry-run \
  --object-image quay.io/my-org/odf-io-stress:dev
```

The printed worker pod must use your image and the `s3-worker` command above.
