FROM golang:1.21-alpine as BUILD
WORKDIR /src

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /bin/app .

FROM gcr.io/distroless/static
COPY --from=BUILD /bin/app /bin/app
ENTRYPOINT [ "/bin/app" ]