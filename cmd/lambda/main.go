package main

import (
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/berniyo/paypack-lambda/internal/handler"
	"github.com/berniyo/paypack-lambda/internal/paypack"
)

func main() {
	client, err := paypack.NewClientFromEnv(nil)
	if err != nil {
		log.Fatalf("failed to configure paypack client: %v", err)
	}

	callbackURL := strings.TrimSpace(os.Getenv("SUBSCRIPTION_CALLBACK_URL"))
	if callbackURL == "" {
		log.Fatal("SUBSCRIPTION_CALLBACK_URL must be set")
	}
	callbackSecret := os.Getenv("SUBSCRIPTION_CALLBACK_SECRET")
	callbackSender, err := handler.NewHTTPSCallbackSender(callbackURL, callbackSecret, nil)
	if err != nil {
		log.Fatalf("failed to configure callback sender: %v", err)
	}

	processor := handler.NewProcessor(client, handler.WithCallbackSender(callbackSender))

	lambda.Start(processor.Handle)
}
