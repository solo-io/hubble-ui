# Deploying the Solo.io Hubble UI

## Automated Deployment

To do an automated deployment of the Solo.io Hubble UI, [create a new release](https://github.com/solo-io/hubble-ui/releases/new), and set the title and tag to the version that you want to release (e.g. `v1.2.3`). After the release is created, the [GitHub action](https://github.com/solo-io/hubble-ui/actions) should be kicked off to build and deploy the project to GCR. The new image will be tagged `gcr.io/solo-public/docs/hubble-ui-frontend:v1.2.3` and `gcr.io/solo-public/docs/hubble-ui-frontend:latest`.

## Manual Deployment

Modify and use the following command to build the Hubble UI frontend, and push to GCR. This assumes that the terminal is opened the repository root.

```sh
export RELEASE_TAG=v0.0.1
export FRONTEND_IMAGE_NAME=gcr.io/solo-public/docs/hubble-ui-frontend
docker build -f ./Dockerfile -t "${FRONTEND_IMAGE_NAME}:latest" -t "${FRONTEND_IMAGE_NAME}:${RELEASE_TAG}" . && \
docker push "${FRONTEND_IMAGE_NAME}:latest" && \
docker push "${FRONTEND_IMAGE_NAME}:${RELEASE_TAG}"
```

Once the images are built and pushed, we can apply the deployment resource.

```sh
k create ns apps
cat ./solo-resources/kubernetes/hubble_ui_frontend_deployment.yaml | envsubst | k apply -f -
```

Then, when the deployment is ready, we can port-forward it to view the hubble-ui. Note that the backend must be applied separately for the frontend to receive data.

```sh
k port-forward -n apps deploy/solo-hubble-ui-frontend 8088:8080
```
