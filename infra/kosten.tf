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

  # Braucht die Billing-Budgets-API; ohne die Abhängigkeit schlägt ein Neuaufbau
  # des Projekts fehl, wenn das Budget vor dem Aktivieren der APIs drankommt.
  depends_on = [google_project_service.apis]
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

# Eigene Rolle statt roles/run.developer: Der Kill-Switch darf Dienste nur
# lesen und ändern, aber keine neuen Dienste oder Jobs anlegen und nichts
# löschen. Grenze: "ändern" umfasst auch das Image. Zusammen mit actAs auf die
# Laufzeitkonten (unten) könnte kompromittierter Code im Kill-Switch ein
# fremdes Image als z. B. Konto "server" starten (siehe docs/stand.md).
resource "google_project_iam_custom_role" "killswitch_cloud_run" {
  role_id     = "killswitchCloudRun"
  title       = "Kill-Switch: Cloud-Run-Dienste sperren"
  description = "Dienste auflisten, lesen und den Ingress ändern"
  permissions = [
    "run.services.get",
    "run.services.list",
    "run.services.update",
    "run.operations.get", # Warten, bis die Änderung aktiv ist
  ]
  depends_on = [google_project_service.apis]
}

locals {
  # Map mit festen Schlüsseln: for_each braucht Schlüssel, die schon beim
  # Plan bekannt sind; der Name der eigenen Rolle entsteht erst beim Anlegen.
  killswitch_rollen = {
    cloud_run  = google_project_iam_custom_role.killswitch_cloud_run.name # Dienste sperren
    abrechnung = "roles/billing.projectManager"                           # Projekt vom Rechnungskonto trennen
    eventarc   = "roles/eventarc.eventReceiver"                           # Ereignisse von Eventarc empfangen
  }

  # Wer einen Cloud-Run-Dienst ändert, muss dessen Laufzeit-Dienstkonto
  # "benutzen" dürfen (actAs). Deshalb bekommt der Kill-Switch dieses Recht
  # nur für genau diese Konten – nicht projektweit.
  # NEUE CLOUD-RUN-DIENSTE: Ihr Laufzeitkonto hier eintragen, sonst kann der
  # Kill-Switch sie nicht sperren (Fehler im Log).
  cloud_run_laufzeitkonten = {
    # Standardkonto für Dienste ohne eigenes Konto (z. B. manuelle Tests).
    compute_standard = "${data.google_project.projekt.number}-compute@developer.gserviceaccount.com"
    server           = google_service_account.server.email
  }
}

resource "google_project_iam_member" "killswitch" {
  for_each = local.killswitch_rollen
  project  = var.projekt
  role     = each.value
  member   = google_service_account.killswitch.member
}

resource "google_service_account_iam_member" "killswitch_act_as" {
  for_each           = local.cloud_run_laufzeitkonten
  service_account_id = "projects/${var.projekt}/serviceAccounts/${each.value}"
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.killswitch.member
  depends_on         = [google_project_service.apis] # das Compute-Standardkonto entsteht mit der Run-API
}

# Ohne Organisation gibt Google den Standardkonten automatisch die Rolle
# Editor. Wer Code unter diesen Konten ausführt, könnte fast alles im Projekt
# ändern. Die Bindungen werden hier entfernt; Dienste und Funktionen laufen
# mit eigenen, schmalen Konten.
resource "google_project_iam_member_remove" "standardkonten_ohne_editor" {
  # Die Standardkonten entstehen erst mit den APIs (Neuaufbau des Projekts).
  depends_on = [google_project_service.apis]

  for_each = toset([
    "serviceAccount:${data.google_project.projekt.number}-compute@developer.gserviceaccount.com",
    "serviceAccount:${var.projekt}@appspot.gserviceaccount.com",
  ])
  project = var.projekt
  role    = "roles/editor"
  member  = each.value
}

# Rechte des Build-Kontos: die von Google dokumentierte Minimalmenge
# (Logs schreiben, Quellen lesen, Image schreiben), aber so eng wie möglich.
# Die breite Rolle roles/cloudbuild.builds.builder ist bewusst nicht dabei: Sie
# erlaubt u. a. Nachrichten auf das Budget-Thema (gefälschte Meldungen).
resource "google_project_iam_member" "bauen_logs" {
  project = var.projekt
  role    = "roles/logging.logWriter"
  member  = google_service_account.bauen.member
}

locals {
  quellen_bucket = "projects/_/buckets/${google_storage_bucket.quellen.name}"
}

# KRITISCH: Lesen nur in unserem Quellen-Bucket und in den Buckets, in die Cloud
# Functions die Quellen beim Deploy kopiert ("gcf-v2-…", legt Google an) –
# nicht im Bucket der Datensicherungen. Lesbare Chiffrate wären zwar ohne
# Schlüssel wertlos, aber das Build-Konto führt fremden Code aus (Go-Module).
resource "google_project_iam_member" "bauen_quellen" {
  project = var.projekt
  role    = "roles/storage.objectViewer"
  member  = google_service_account.bauen.member

  condition {
    title       = "nur-quellen"
    description = "Nur der Quellen-Bucket und die Buckets von Cloud Functions"
    expression = join(" || ", [
      "resource.name == \"${local.quellen_bucket}\"",
      "resource.name.startsWith(\"${local.quellen_bucket}/\")",
      "resource.name.startsWith(\"projects/_/buckets/gcf-v2-\")",
    ])
  }
}

# Images nur ins Repo "funktionen" (nicht "container", aus dem der Server läuft).
resource "google_artifact_registry_repository_iam_member" "bauen_funktionen" {
  location   = google_artifact_registry_repository.funktionen.location
  repository = google_artifact_registry_repository.funktionen.name
  role       = "roles/artifactregistry.writer"
  member     = google_service_account.bauen.member
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

locals {
  # Nur ausdrücklich erlaubte Dateien gehen in die Cloud: Go-Code ohne Tests
  # plus go.mod/go.sum. Mit dem ganzen Ordner könnten lokale Dateien (z. B.
  # eine .env oder Notizen) ungewollt in GCP landen.
  killswitch_dateien = setunion(
    setsubtract(fileset("${path.module}/killswitch", "*.go"), fileset("${path.module}/killswitch", "*_test.go")),
    ["go.mod", "go.sum"],
  )
}

data "archive_file" "killswitch" {
  type        = "zip"
  output_path = "${path.module}/.build/killswitch.zip"

  dynamic "source" {
    for_each = local.killswitch_dateien
    content {
      filename = source.value
      content  = file("${path.module}/killswitch/${source.value}")
    }
  }
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
    docker_repository = google_artifact_registry_repository.funktionen.id
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
    google_project_iam_member.bauen_logs,
    google_project_iam_member.bauen_quellen,
    google_artifact_registry_repository_iam_member.bauen_funktionen,
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

# Die Schlüssel der Rollen-Bindungen haben sich geändert (Rollenname → kurzer
# Name). "moved" sagt OpenTofu, dass es dieselben Bindungen sind – sonst würde
# es sie löschen und neu anlegen, und in der Zwischenzeit fehlten die Rechte.
moved {
  from = google_project_iam_member.killswitch["roles/billing.projectManager"]
  to   = google_project_iam_member.killswitch["abrechnung"]
}

moved {
  from = google_project_iam_member.killswitch["roles/eventarc.eventReceiver"]
  to   = google_project_iam_member.killswitch["eventarc"]
}

# Die Log-Rolle des Build-Kontos ist dieselbe Bindung wie vorher, nur unter
# eigenem Namen; die beiden anderen Rollen sind jetzt enger (oben).
moved {
  from = google_project_iam_member.bauen["roles/logging.logWriter"]
  to   = google_project_iam_member.bauen_logs
}
