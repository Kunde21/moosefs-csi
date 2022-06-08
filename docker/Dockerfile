ARG MOOSEFS_IMAGE=kunde21/moosefs
ARG MOOSEFS_TAG=v3.0.116
FROM golang:1.18.3-alpine3.16 AS build-env

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

RUN apk add moosefs --repository=http://dl-cdn.alpinelinux.org/alpine/edge/testing/

ENTRYPOINT ["/bin/moosefs-csi-plugin"]
