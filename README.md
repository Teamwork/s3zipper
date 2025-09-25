# s3zipper
Microservice that serves a streaming zip file of files securely downloaded from S3

## Read the blog here
[Original Blog Post](https://engineroom.teamwork.com/how-to-securely-provide-a-zip-download-of-a-s3-file-bundle-1c3c779ae3f1)

# s3zipper-builder
An app that scans for changes and deploys the new version to ArgoCD:
https://github.com/Teamwork/s3zipper-builder

# AWS AUTH
To authenticate to AWS you can use either AWS_ACCESS_KEY_ID with AWS_SECRET_ACCESS_KEY method. You would then need to add them to conf.json:
```
{
	"AccessKey": "key",
	"SecretKey": "key",
	"Bucket": "bucket",
	"Region": "us-east-1",
	"RedisServerAndPort": "127.0.0.1:6379",
	"Port": 8000
}
```
If you wont add the keys to the conf file, then it will fall back to the default role based authentication (if the role is not applied to the container it will fail):
```
{

	"Bucket": "bucket",
	"Region": "us-east-1",
	"RedisServerAndPort": "127.0.0.1:6379",
	"Port": 8000
}
```

