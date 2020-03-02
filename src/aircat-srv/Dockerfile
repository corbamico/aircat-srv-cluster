FROM    golang:alpine as OnBuilder
#FROM    golang:buster as OnBuilder
WORKDIR /src/aircat-srv
COPY    go.mod  ./
COPY    go.sum  ./
RUN     go env -w GOPROXY=https://goproxy.cn,direct
RUN     go mod download
COPY    . ./
RUN     go build -o /app/aircat-srv .

FROM    alpine
COPY    --from=OnBuilder /app/aircat-srv  /app/aircat-srv
ENTRYPOINT ["/app/aircat-srv"]