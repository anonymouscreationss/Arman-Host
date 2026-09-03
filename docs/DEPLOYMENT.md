# ARMAN deployment plan

Deployment is not enabled yet. The first production deployment requires:

- a production PostgreSQL database
- Redis
- object storage
- AI provider configuration
- email delivery
- Firebase project configuration
- production CORS origins
- HTTPS
- release signing configuration for Android

No signing keys should be generated or committed by the repository bootstrap.
