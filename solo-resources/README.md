# Deploying the Hubble UI

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
