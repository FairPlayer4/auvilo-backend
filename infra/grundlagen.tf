# APIs, Datenbank, Container-Registry und Logs in der EU.

data "google_project" "projekt" {}

locals {
  apis = [
    "artifactregistry.googleapis.com",
    "billingbudgets.googleapis.com",
    "cloudbilling.googleapis.com",
    "cloudbuild.googleapis.com",
    "cloudfunctions.googleapis.com",
    "eventarc.googleapis.com",
    "firestore.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "logging.googleapis.com",
    "monitoring.googleapis.com",
    "pubsub.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
    "sts.googleapis.com",
  ]
}

resource "google_project_service" "apis" {
  for_each = toset(local.apis)
  service  = each.value

  # Beim Löschen der Konfiguration die APIs eingeschaltet lassen; abgeschaltet
  # wird mit dem Projekt.
  disable_on_destroy = false
}

# --- Firestore ---------------------------------------------------------------
# Die "(default)"-Datenbank ist die einzige mit Gratis-Kontingent.
resource "google_firestore_database" "standard" {
  name                    = "(default)"
  location_id             = var.region
  type                    = "FIRESTORE_NATIVE"
  delete_protection_state = "DELETE_PROTECTION_DISABLED"
  deletion_policy         = "DELETE" # Lernprojekt: muss vollständig löschbar sein

  depends_on = [google_project_service.apis]
}

# --- Container-Registry ------------------------------------------------------
# Hier landen die Images des Servers und der Funktionen. Aufräumregeln halten
# den Speicher unter dem Gratis-Kontingent (0,5 GB).
resource "google_artifact_registry_repository" "container" {
  location      = var.region
  repository_id = "container"
  format        = "DOCKER"
  description   = "Container-Images für avlreminder-backend"

  cleanup_policy_dry_run = false

  # BEHALTEN hat Vorrang vor LÖSCHEN: Die 3 neuesten Versionen je Image bleiben
  # immer, alles andere wird nach 7 Tagen gelöscht.
  cleanup_policies {
    id     = "neueste-behalten"
    action = "KEEP"
    most_recent_versions {
      keep_count = 3
    }
  }
  cleanup_policies {
    id     = "alte-loeschen"
    action = "DELETE"
    condition {
      older_than = "604800s"
    }
  }

  depends_on = [google_project_service.apis]
}

# --- Logs in der EU ----------------------------------------------------------
# Ohne Organisation liegt der Standard-Log-Bucket "_Default" in "global".
# Deshalb leiten wir alle Logs in einen eigenen Bucket in der EU-Region um und
# schließen sie vom _Default-Bucket aus. Nur die Admin-Audit-Logs ("_Required")
# lassen sich nicht umleiten; sie enthalten keine Inhalte, nur Verwaltungsaktionen.
locals {
  # Gleicher Filter wie die _Default-Senke: alles außer den Pflicht-Audit-Logs.
  log_filter = join(" AND ", [for id in [
    "cloudaudit.googleapis.com/activity",
    "externalaudit.googleapis.com/activity",
    "cloudaudit.googleapis.com/system_event",
    "externalaudit.googleapis.com/system_event",
    "cloudaudit.googleapis.com/access_transparency",
    "externalaudit.googleapis.com/access_transparency",
  ] : "NOT LOG_ID(\"${id}\")"])
}

resource "google_logging_project_bucket_config" "eu" {
  project        = var.projekt
  location       = var.region
  bucket_id      = "logs-eu"
  retention_days = 30 # Anforderung P5
  description    = "Alle Logs des Projekts, EU, 30 Tage"
}

resource "google_logging_project_sink" "eu" {
  name                   = "logs-eu"
  destination            = "logging.googleapis.com/${google_logging_project_bucket_config.eu.id}"
  filter                 = local.log_filter
  unique_writer_identity = true
}

# Eine Projekt-Ausnahme gilt für die _Default-Senke: Diese Logs landen nicht
# mehr im globalen Bucket.
resource "google_logging_project_exclusion" "nicht_global" {
  name        = "nicht-global"
  description = "Logs nur im EU-Bucket speichern"
  filter      = local.log_filter
}
