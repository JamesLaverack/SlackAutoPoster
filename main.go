package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"google.golang.org/api/iterator"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/firestore"
	cloudkms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/storage"
	"github.com/nlopes/slack"
	kmspb "google.golang.org/genproto/googleapis/cloud/kms/v1"
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.Print("Hello world received a request.")

	oauthTokenKeyName := os.Getenv("OAUTH_TOKEN_KEY_NAME")
	if oauthTokenKeyName == "" {
		log.Fatal("Key name to decrypt the Slack OAuth token not provided")
	}

	encryptedEncodedOauthToken := os.Getenv("OAUTH_TOKEN")
	if encryptedEncodedOauthToken == "" {
		log.Fatal("Slack OAuth Token not provided")
	}

	messageBucketName := os.Getenv("MESSAGE_BUCKET_NAME")
	if messageBucketName == "" {
		log.Fatal("Message bucket name not provided")
	}

	encryptedOauthToken, err := base64.StdEncoding.DecodeString(encryptedEncodedOauthToken)
	if err != nil {
		log.Fatal(err)
	}

	kmsClient, err := cloudkms.NewKeyManagementClient(r.Context())
	if err != nil {
		log.Fatal("Failed to contact Cloud KMS", err)
	}

	decryptRequest := &kmspb.DecryptRequest{
		Name:       oauthTokenKeyName,
		Ciphertext: encryptedOauthToken,
	}

	resp, err := kmsClient.Decrypt(r.Context(), decryptRequest)
	if err != nil {
		log.Fatal("Failed to decrypt key", err)
	}

	slackClient := slack.New(string(resp.Plaintext))

	//_, _, err = slackClient.PostMessage("flight-gauge-scratch", slack.MsgOptionText("Cloud Run Invoked v2", false))
	//if err != nil {
	//	log.Fatal("Failed talking to Slack", err)
	//}

	projectID, err := metadata.ProjectID()
	if err != nil {
		log.Fatal("Failed to get current project ID", err)
	}

	firestoreClient, err := firestore.NewClient(r.Context(), projectID)
	if err != nil {
		log.Fatal("Failed to connect to Cloud Firestore", err)
	}
	defer firestoreClient.Close()

	scheduledMessages := firestoreClient.Collection("scheduled-slack-messages")

	log.Println("Searching for messages to post in Slack.")

	query := scheduledMessages.Where("posted?", "==", false).Where("when", "<", time.Now())

	storageClient, err := storage.NewClient(r.Context())
	if err != nil {
		log.Panic("Failed to connect to Cloud Storage")
	}

	messageBucket := storageClient.Bucket(messageBucketName)

	type ScheduledMessage struct {
		Channel       string
		Message       string
		MessageObject string
	}

	iter := query.Documents(r.Context())
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatal("Failed iterating through Cloud Firestore", err)
		}
		log.Printf("Seen message %v", doc)
		m := ScheduledMessage{}
		err = doc.DataTo(&m)
		if err != nil {
			log.Fatal("Failed to parse scheduled message", err)
		}
		log.Printf("Parsed message %v", m)
		log.Println("Sending message to Slack")
		messageText := ""
		if m.Message != "" {
			// Raw message
			messageText = m.Message
		} else {
			// Cloud storage bucket message
			obj := messageBucket.Object(m.MessageObject)
			r, err := obj.NewReader(r.Context())
			if err != nil {
				log.Fatal("Failed to read expected object from Google Stroage")
			}

			buf := new(bytes.Buffer)
			buf.ReadFrom(r)
			messageText = buf.String()
		}

		// Actually fire to slack
		if messageText == "" {
			log.Fatal("No message to post?")
		}
		_, _, err = slackClient.PostMessage(m.Channel, slack.MsgOptionText(messageText, false))
		if err != nil {
			log.Fatal("Failed talking to Slack", err)
		}

		log.Println("Updated Cloud Firestore")
		_, err = doc.Ref.Update(r.Context(), []firestore.Update{
			{
				Path:  "posted?",
				Value: true,
			},
		})
		if err != nil {
			log.Fatal("Failed to update Cloud Firestore", err)
		}
	}
	log.Println("Finished Posting messages to Slack")
}

func main() {
	log.Print("Hello world sample started.")

	http.HandleFunc("/", handler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}
