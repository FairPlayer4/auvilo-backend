# Budget und Kill-Switch (Anforderungen KO2, KO3).
#
# Ablauf: Das Budget schickt mehrmals täglich den aktuellen Stand an ein
# Pub/Sub-Thema. Die Funktion "killswitch" (Go, infra/killswitch) liest ihn und
# sperrt ab 100 % alle Cloud-Run-Dienste, ab 150 % trennt sie das Projekt vom
# Rechnungskonto.

resource "google_pubsub_topic" "budget" {
  name = "budget-meldungen"

  # Pub/Sub-Themen sind global; ohne diese Regel dürfte Google Nachrichten
  # außerhalb der EU speichern (Anforderung D1).
  message_storage_policy {
    allowed_persistence_regions = [var.region]
  }

  depends_on = [google_project_service.apis]
}

resource "google_billing_budget" "monat" {
  billing_account = var.rechnungskonto
  display_name    = "${var.projekt} monatlich"

  budget_filter {
    # Nur dieses Projekt – andere Projekte am selben Rechnungskonto zählen nicht.
    projects        = ["projects/${data.google_project.projekt.number}"]
    calendar_period = "MONTH"
    # Kosten VOR Abzug von Guthaben zählen. Sonst würde das Test-Guthaben
    # echte Ausgaben verdecken, und der Kill-Switch griffe nie.
    credit_types_treatment = "EXCLUDE_ALL_CREDITS"
  }

  amount {
    specified_amount {
      currency_code = "EUR"
      units         = tostring(var.budget_eur)
    }
  }

  # E-Mail-Warnungen an die Abrechnungsadministratoren.
  dynamic "threshold_rules" {
    for_each = [0.25, 0.5, 0.9, 1.0]
    content {
      threshold_percent = threshold_rules.value
    }
  }
  threshold_rules {
    threshold_percent = 1.0
    spend_basis       = "FORECASTED_SPEND" # Warnung, wenn die Prognose 100 % erreicht
  }

  all_updates_rule {
    pubsub_topic = google_pubsub_topic.budget.id
  }
}

# --- Dienstkonten ------------------------------------------------------------

# Unter diesem Konto läuft die Funktion. Es darf nur, was der Kill-Switch braucht.
resource "google_service_account" "killswitch" {
  account_id   = "killswitch"
  display_name = "Kill-Switch (Budget)"
  depends_on   = [google_project_service.apis]
}

# Unter diesem Konto baut Cloud Build die Funktion.
resource "google_service_account" "bauen" {
  account_id   = "funktionen-bauen"
  display_name = "Cloud Build für Funktionen"
  depends_on   = [google_project_service.apis]
}

locals {
  killswitch_rollen = [
    "roles/run.developer",          # Cloud-Run-Dienste ändern (Ingress)
    "roles/iam.serviceAccountUser", # nötig, um Dienste mit ihrem Dienstkonto zu aktualisieren
    "roles/billing.projectManager", # Projekt vom Rechnungskonto trennen
    "roles/eventarc.eventReceiver", # Ereignisse von Eventarc empfangen
  ]
  bauen_rollen = [
    "roles/cloudbuild.builds.builder",
    "roles/logging.logWriter",
    "roles/artifactregistry.writer",
    "roles/storage.objectViewer",
  ]
}

resource "google_project_iam_member" "killswitch" {
  for_each = toset(local.killswitch_rollen)
  project  = var.projekt
  role     = each.value
  member   = google_service_account.killswitch.member
}

resource "google_project_iam_member" "bauen" {
  for_each = toset(local.bauen_rollen)
  project  = var.projekt
  role     = each.value
  member   = google_service_account.bauen.member
}

# --- Quellcode der Funktion --------------------------------------------------

resource "google_storage_bucket" "quellen" {
  name                        = "${var.projekt}-quellen"
  location                    = var.region
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  force_destroy               = true

  # Bewusst keine Lebenszyklusregel: Sie würde auch das gerade genutzte
  # Quellarchiv löschen, und `tofu plan` zeigte danach eine Abweichung.
  # Die Archive sind wenige KB groß.
}

data "archive_file" "killswitch" {
  type        = "zip"
  source_dir  = "${path.module}/killswitch"
  output_path = "${path.module}/.build/killswitch.zip"
  excludes    = ["*_test.go"]
}

resource "google_storage_bucket_object" "killswitch" {
  # Der Hash im Namen sorgt dafür, dass jede Code-Änderung neu deployt wird.
  name   = "killswitch-${data.archive_file.killswitch.output_md5}.zip"
  bucket = google_storage_bucket.quellen.name
  source = data.archive_file.killswitch.output_path
}

# --- Funktion ----------------------------------------------------------------

resource "google_cloudfunctions2_function" "killswitch" {
  name     = "killswitch"
  location = var.region

  build_config {
    runtime           = "go127"
    entry_point       = "KillSwitch"
    service_account   = google_service_account.bauen.id
    docker_repository = google_artifact_registry_repository.container.id
    source {
      storage_source {
        bucket = google_storage_bucket.quellen.name
        object = google_storage_bucket_object.killswitch.name
      }
    }
  }

  service_config {
    available_memory      = "128Mi"
    max_instance_count    = 1
    timeout_seconds       = 120
    service_account_email = google_service_account.killswitch.email
    ingress_settings      = "ALLOW_INTERNAL_ONLY"
    environment_variables = {
      PROJEKT          = var.projekt
      REGION           = var.region
      SCHWELLE_SPERREN = tostring(var.schwelle_sperren)
      SCHWELLE_TRENNEN = tostring(var.schwelle_trennen)
    }
  }

  event_trigger {
    trigger_region        = var.region
    event_type            = "google.cloud.pubsub.topic.v1.messagePublished"
    pubsub_topic          = google_pubsub_topic.budget.id
    service_account_email = google_service_account.killswitch.email
    # Keine Wiederholung: Das Budget meldet sich ohnehin mehrmals täglich neu.
    retry_policy = "RETRY_POLICY_DO_NOT_RETRY"
  }

  depends_on = [
    google_project_iam_member.bauen,
    google_project_iam_member.killswitch,
  ]
}

# Eventarc ruft die Funktion mit dem Kill-Switch-Konto auf; es braucht dafür
# das Aufrufrecht nur für diesen einen Dienst.
resource "google_cloud_run_service_iam_member" "killswitch_aufrufen" {
  location = var.region
  service  = google_cloudfunctions2_function.killswitch.name
  role     = "roles/run.invoker"
  member   = google_service_account.killswitch.member
}
