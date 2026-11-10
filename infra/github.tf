# Workload Identity Federation: GitHub Actions meldet sich ohne Schlüsseldatei
# bei GCP an. GitHub stellt pro Lauf ein signiertes Token aus; GCP prüft, dass
# es aus unserem Repo und vom Branch main kommt, und tauscht es gegen
# kurzlebige Zugangsdaten des Dienstkontos "deploy".

resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github"
  display_name              = "GitHub Actions"
  depends_on                = [google_project_service.apis]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-actions"
  display_name                       = "GitHub Actions OIDC"

  attribute_mapping = {
    "google.subject"          = "assertion.sub"
    "attribute.repository"    = "assertion.repository"
    "attribute.repository_id" = "assertion.repository_id"
    "attribute.ref"           = "assertion.ref"
  }

  # Nur unser Repo, nur der Branch main und nur der Deploy-Workflow von main
  # dürfen sich anmelden (Empfehlung E3: sonst könnte jeder andere Workflow auf
  # main deployen, etwa einer ohne CI-Prüfung). Geprüft wird die feste Repo-ID
  # statt des Namens (siehe var.github_repo_id); workflow_ref enthält den Namen,
  # zusammen mit der ID ist das eindeutig.
  attribute_condition = join(" && ", [
    "assertion.repository_id == '${var.github_repo_id}'",
    "assertion.ref == 'refs/heads/main'",
    "assertion.workflow_ref == '${var.github_repo}/.github/workflows/deploy.yml@refs/heads/main'",
  ])

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_service_account" "deploy" {
  account_id   = "deploy"
  display_name = "Deploy aus GitHub Actions"
  depends_on   = [google_project_service.apis]
}

resource "google_service_account_iam_member" "deploy_github" {
  service_account_id = google_service_account.deploy.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository_id/${var.github_repo_id}"
}

# Das Deploy-Konto darf Images nur in dieses Repo hochladen (nicht in
# "funktionen"). Seine Rechte auf den Dienst "server" stehen in server.tf.
resource "google_artifact_registry_repository_iam_member" "deploy_schreiben" {
  location   = google_artifact_registry_repository.container.location
  repository = google_artifact_registry_repository.container.name
  role       = "roles/artifactregistry.writer"
  member     = google_service_account.deploy.member
}
