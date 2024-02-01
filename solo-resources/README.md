# Publishing the Solo.io Hubble UI Image

## Automated Method

To publish an image of the Solo.io Hubble UI, [create a new release](https://github.com/solo-io/solo-hubble-ui/releases/new), and set both the release title and release tag to the version that you want to release (e.g. `v1.2.3`). After the release is created, the [GitHub action](https://github.com/solo-io/solo-hubble-ui/actions) should be automatically kicked off to build the image and push the image to GCR. The new image will be tagged `gcr.io/solo-public/docs/solo-hubble-ui-frontend:v1.2.3` and `gcr.io/solo-public/docs/solo-hubble-ui-frontend:latest`.

## Manual Method

Modify and use the following command to build the Hubble UI frontend, and push to GCR. This assumes that the terminal is opened the repository root.

```sh
export RELEASE_TAG=v0.0.1
export FRONTEND_IMAGE_NAME=gcr.io/solo-public/docs/solo-hubble-ui-frontend
docker build -f ./solo-resources/docker/Dockerfile -t "${FRONTEND_IMAGE_NAME}:latest" -t "${FRONTEND_IMAGE_NAME}:${RELEASE_TAG}" . && \
docker push "${FRONTEND_IMAGE_NAME}:latest" && \
docker push "${FRONTEND_IMAGE_NAME}:${RELEASE_TAG}"
```

# Deploying the Solo.io Hubble UI

In order to test that the image was pushed correctly, we can apply the deployment resource in a local cluster.

```sh
k create ns apps
cat ./solo-resources/kubernetes/solo_hubble_ui_frontend_deployment.yaml | envsubst | k apply -f -
```

Then, when the deployment is ready, we can port-forward it to view the hubble-ui. Note that the backend must be applied separately for the frontend to receive data.

```sh
k port-forward -n apps deploy/solo-hubble-ui-frontend 8088:8080
```

_As we come up with a way to serve Hubble UI backend, these deployment steps may change._
