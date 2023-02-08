# use a minimal alpine image
FROM --platform=$BUILDPLATFORM alpine:3.7
# add ca-certificates in case you need them
RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
# set working directory

ARG TARGETOS
ARG TARGETARCH
COPY .build/${TARGETOS}-${TARGETARCH}/amtool       /bin/amtool
COPY .build/${TARGETOS}-${TARGETARCH}/alertmanager /bin/alertmanager
COPY examples/ha/alertmanager.yml      /etc/alertmanager/alertmanager.yml

RUN mkdir -p /alertmanager

WORKDIR /alertmanager

EXPOSE     9093
VOLUME     [ "/alertmanager" ]
WORKDIR    /alertmanager
ENTRYPOINT [ "/bin/alertmanager" ]
CMD        [ "--queryService.url=https://localhost:8080", \
             "--storage.path=/alertmanager" ]
