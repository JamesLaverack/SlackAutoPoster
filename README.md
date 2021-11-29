STATUS: ARCHIVED

This has been nicely depricated by Slack adding a "schedule message" feature.

# Slack AutoPoster

## What?

Write your Slack messages in advance, then have a bot post them for you but they look like you wrote them. The Slack UI
at least will not mark these messages as "BOT" or anything like that.

## Why?

I got told I needed to report something on Slack every day. I knew what I was going to report for a whole week in
advance, so I made this.

### Didn't this take more time than just posting it yourself?

Yes. But that's not the point. It's the principle of the thing.

## How?

Google Cloud.

- Cloud Build to build this code
- Google Container Registry to hold some code
- Cloud Run to execute some code (this repo)
- Cloud Scheduler to wake the Cloud Run every so often
- Cloud KMS to keep my Slack OAuth token safe
- Cloud Firestore to record details about what messages to post, and when to post them.
- Cloud Storage to store the text of longer, multi-line messages.

In theory this is all going to be in the free tier, but I make zero assurances if you're going to run this yourself. You
are responsible for your own billing.

The Cloud Scheduler job wakes up the Cloud Run function every five minutes. On waking the function will do this:

1. Decrypt the OAuth token (in it's environment variables) using KMS
2. Query Firebase looking for messages with a time in the past that haven't been marked as posted

For each firebase document found:
1. If Firebase document has a `message` field on it, post it directly. Otherwise look in the configured Cloud Storage
   bucket for an object, using the field `messageObject` from the Firebase document, and post the contents to Slack.
2. Write an update to Firebase, marking the message as posted by setting `posted?` on the document to be `true`.
   
There is a case where if execution fails between the Slack post and updating Firebase that it might post it *again*
later. I also tested this like once so you're on your own.

## Installing

All installation is manual because I haven't automated that bit yet.

In Slack:

1. Create a new Slack App in your workspace
2. Grant it the `chat:write:user` OAuth scope
3. Install it
4. Grab the OAuth token from your App's settings

Be **very careful** with that token. It lets anyone impersonate you in Slack. So if someone gets it and sends rude
messages to your boss don't blame me.

In Google Cloud:

1. Build this repository and upload it to your GCR using Cloud Build
   ```
   gcloud builds submit --tag gcr.io/$PROJECTID/slack-autoposter
   ```
2. Create a Service Account `slack-autoposter`, Grant it the "Cloud Datastore User" role (for Firebase later)
3. Create a Google Cloud Storage bucket for your long-form posts. Grant the service account read access to it.
4. Create a KMS keyring and key to encrypt your OAuth token. Grant the service account decrypt permissions on it.
5. Encrypt your token with the key:
   ```
   echo -n "SLACK_OAUTH_TOKEN" | gcloud kms encrypt --plaintext-file=- --ciphertext-file=- --location $KEYLOC --keyring $KEYRINGNAME --key $KEYNAME | base64
   ```
6. Create a Firebase database, use native mode
7. Create a Firebase collection named `scheduled-slack-messages` (the name is actually critical here because it's
   hard-coded. Sorry not sorry.) Add an index on the collection for two fields:
   
   * `posted?` ASC
   * `when` ASC
8. Create a Cloud Run Service using the container you made in step 1, set it to require authentication, make it use the
   service account you created above.
   
   Set environment variables like this:
   * `OAUTH_TOKEN` to the encrypted token you got from step 5.
   * `OAUTH_TOKEN_KEY_NAME` to the full path to the key you created in step 4.
   * `MESSAGE_BUCKET_NAME` to the name of the bucket you created in step 3
9. Create a new service account `slack-autoposter-invoker`. Grant it permissions to invoke your cloud run service.
10. Create a job in Cloud Scheduler on a `*/5 * * * *` (or whatever) cron schedule. It should use HTTP to hit the
    endpoint of the cloud service you created in step 8. You must set the header to "Add OIDC Token", specify the
    full email of the service account you created in step 9, and set the audiance to be the same URL of your Cloud Run
    service

## Configuring Messages

To schedule a message, add it to Firestore as a document under the `scheduled-slack-messages`. You can either put the
message as a field `message`, or put it in an object in the Google Storage bucket you made earlier and put the object's
name as a field `messageObject`.

Additionally. The Firestore document *must* have:

* A `when` field, as a timestamp, of when you wanted it to be posted. Note that this is 'best effort' and will only
  actually get posted at five minute resolution.
* A `posted?` field, as a bool, which you should set to `false`.
* A `channel` field, as a string, which must be the name of the channel you want to post to. See
  [Slack's documentation](https://api.slack.com/methods/chat.postMessage) for what this means exactly in some edge cases
  like IM chats.
