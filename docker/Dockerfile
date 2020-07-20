ARG MOOSEFS_IMAGE
ARG MOOSEFS_TAG
FROM golang:1.15-alpine3.12 AS build-env

ARG MOOSEFS_VERSION=development
ARG GOPROXY=https://proxy.golang.org
ARG GOARM=7

ENV CGO_ENABLED=0
WORKDIR /build

COPY . .

RUN go build -trimpath -v -o /moosefs-csi-plugin -a -ldflags "-s -w -X 'mfs.version=$MOOSEFS_VERSION' --extldflags '-static'" ./moosefs-csi

FROM ${MOOSEFS_IMAGE}:${MOOSEFS_TAG}
LABEL container_build="Chad Kunde <Kunde21@gmail.com>"
LABEL description="MOOSEFS CSI plugin"

COPY --from=build-env /moosefs-csi-plugin /bin/moosefs-csi-plugin

ENTRYPOINT ["/bin/moosefs-csi-plugin"]
