# Regelmäßiger Lauf: löscht Konten mit fälligem Termin (Konto löschen, B1 in
# docs/anforderungen.md Kap. 3.5; docs/b2b-entwurf.md „Konto löschen“), später
# auch nach den Fristen aus P5.
#
# Dasselbe Image wie der Server, mit dem Unterbefehl "aufraeumen"
# (server/cmd/server): Er läuft einmal durch und endet. Deshalb ein Cloud Run
# Job statt eines Dienstes; nicht öffentlich, Cloud Scheduler startet ihn
# täglich. Das Image setzt wie beim Server der Deploy (deploy.yml).
#
# Kosten: Der Job läuft einmal am Tag wenige Sekunden (im kostenlosen Kontingent
# von Cloud Run); Cloud Scheduler hat drei Jobs je Rechnungskonto frei; der Alarm
# kostet wie die anderen Log-Alarme ca. 0,10 $ im Monat.

# --- Rechte: Objekte auflisten und löschen, nie lesen -------------------------

# Für Konto löschen in beiden Buckets: die Objekte (bei Datensicherungen alle
# Fassungen) eines Kontos finden und löschen. Ohne storage.objects.get: Wer nur
# diese Rolle hat, liest keinen Inhalt (Namen und Größen sieht er).
resource "google_project_iam_custom_role" "objekte_loeschen" {
  role_id     = "objekteLoeschen"
  title       = "Objekte auflisten und löschen (Konto löschen)"
  permissions = ["storage.objects.list", "storage.objects.delete"]
  depends_on  = [google_project_service.apis]
}

# Der Server löscht beim sofortigen Konto löschen auch die Diagnosen; in der
# Datensicherung darf er das schon über objectUser (datensicherung.tf).
resource "google_storage_bucket_iam_member" "server_diagnose_loeschen" {
  bucket = google_storage_bucket.diagnose.name
  role   = google_project_iam_custom_role.objekte_loeschen.name
  member = google_service_account.server.member
}

# --- Dienstkonto des Laufs ---------------------------------------------------

# Eigenes Konto: Firestore und in beiden Buckets nur auflisten und löschen.
# Kein Pfeffer, keine Datensicherung lesen, nichts anlegen.
resource "google_service_account" "aufraeumen" {
  account_id   = "aufraeumen"
  display_name = "Aufräumen (Cloud Run Job)"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "aufraeumen_firestore" {
  project = var.projekt
  role    = "roles/datastore.user"
  member  = google_service_account.aufraeumen.member
}

resource "google_storage_bucket_iam_member" "aufraeumen" {
  for_each = {
    datensicherung = google_storage_bucket.datensicherung.name
    diagnose       = google_storage_bucket.diagnose.name
  }
  bucket = each.value
  role   = google_project_iam_custom_role.objekte_loeschen.name
  member = google_service_account.aufraeumen.member
}

# --- Job ---------------------------------------------------------------------

resource "google_cloud_run_v2_job" "aufraeumen" {
  name     = "aufraeumen"
  location = var.region
  # Hält keine Daten, nur die Konfiguration; tofu darf ihn ersetzen.
  deletion_protection = false

  template {
    task_count = 1

    template {
      service_account = google_service_account.aufraeumen.email
      # Ein Lauf braucht Sekunden; 10 Minuten reichen auch für viele Konten.
      timeout = "600s"
      # Ein zweiter Versuch schadet nicht: Jeder Schritt ist wiederholbar.
      max_retries = 1

      containers {
        # Beim ersten Anlegen das Image, das der Server gerade hat (der Deploy
        # setzt es dort; OpenTofu kennt es aus dem Zustand). Danach setzt der
        # Deploy es hier mit.
        image = google_cloud_run_v2_service.server.template[0].containers[0].image
        args  = ["aufraeumen"]

        # Jobs laufen immer in der zweiten Generation; die verlangt mindestens
        # 512 MiB. Der Lauf dauert Sekunden, die Kosten bleiben vernachlässigbar.
        resources {
          limits = {
            cpu    = "1"
            memory = "512Mi"
          }
        }

        env {
          name  = "PROJEKT"
          value = var.projekt
        }
        env {
          name  = "SICHERUNG_BUCKET"
          value = google_storage_bucket.datensicherung.name
        }
        env {
          name  = "DIAGNOSE_BUCKET"
          value = google_storage_bucket.diagnose.name
        }
        env {
          name  = "GOMEMLIMIT"
          value = "384MiB"
        }
      }
    }
  }

  lifecycle {
    # image: setzt der Deploy. client*: setzt gcloud.
    ignore_changes = [
      template[0].template[0].containers[0].image,
      client,
      client_version,
    ]
  }

  depends_on = [
    google_project_iam_member.aufraeumen_firestore,
    google_storage_bucket_iam_member.aufraeumen,
  ]
}

# --- Täglich starten ---------------------------------------------------------

# Das Konto, mit dem Cloud Scheduler den Job startet. Es darf nur das.
resource "google_service_account" "planer" {
  account_id   = "planer"
  display_name = "Cloud Scheduler: startet den Job aufraeumen"
  depends_on   = [google_project_service.apis]
}

# roles/run.invoker enthält run.jobs.run; nur für diesen Job.
resource "google_cloud_run_v2_job_iam_member" "planer_aufraeumen" {
  location = google_cloud_run_v2_job.aufraeumen.location
  name     = google_cloud_run_v2_job.aufraeumen.name
  role     = "roles/run.invoker"
  member   = google_service_account.planer.member
}

resource "google_cloud_scheduler_job" "aufraeumen" {
  name      = "aufraeumen"
  region    = var.region
  schedule  = "0 4 * * *" # täglich 4 Uhr
  time_zone = "Europe/Berlin"

  # Der Aufruf startet den Job nur und wartet nicht auf sein Ende.
  attempt_deadline = "60s"
  retry_config {
    retry_count = 2
  }

  http_target {
    http_method = "POST"
    # Regionaler Endpunkt (Admin-API v1, wie die Konsole ihn für Scheduler nutzt).
    uri = "https://${google_cloud_run_v2_job.aufraeumen.location}-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/${var.projekt}/jobs/${google_cloud_run_v2_job.aufraeumen.name}:run"

    oauth_token {
      service_account_email = google_service_account.planer.email
      scope                 = "https://www.googleapis.com/auth/cloud-platform"
    }
  }

  depends_on = [google_cloud_run_v2_job_iam_member.planer_aufraeumen]
}

# --- Deploy aus GitHub Actions -----------------------------------------------

# Das Deploy-Konto setzt das neue Image auch im Job …
resource "google_cloud_run_v2_job_iam_member" "deploy_aufraeumen" {
  location = google_cloud_run_v2_job.aufraeumen.location
  name     = google_cloud_run_v2_job.aufraeumen.name
  role     = "roles/run.developer"
  member   = google_service_account.deploy.member
}

# … und darf dabei nur das Konto des Laufs einsetzen (actAs).
resource "google_service_account_iam_member" "deploy_act_as_aufraeumen" {
  service_account_id = google_service_account.aufraeumen.name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.deploy.member
}

# --- Alarm -------------------------------------------------------------------

# Scheitert ein Lauf, bleiben fällige Konten stehen (DSGVO Art. 17). Der Lauf
# loggt Fehler als ERROR (server/cmd/server), Cloud Run ein gescheitertes
# Ausführen ebenfalls.
resource "google_monitoring_alert_policy" "aufraeumen" {
  display_name = "Regelmäßiger Lauf aufraeumen ist gescheitert"
  combiner     = "OR"

  conditions {
    display_name = "Log des Jobs mit ERROR oder schlimmer"
    condition_matched_log {
      filter = join(" AND ", [
        "resource.type=\"cloud_run_job\"",
        "resource.labels.job_name=\"${google_cloud_run_v2_job.aufraeumen.name}\"",
        "severity>=ERROR",
      ])
    }
  }

  alert_strategy {
    notification_rate_limit {
      period = "3600s"
    }
    auto_close = "86400s"
  }

  documentation {
    mime_type = "text/markdown"
    content   = <<-EOT
      Der tägliche Lauf `aufraeumen` (fällige Konto-Löschungen) ist gescheitert.
      Die betroffenen Konten bleiben fällig; der nächste Lauf versucht es erneut.

      - Logs: `gcloud logging read 'resource.labels.job_name="aufraeumen"' --bucket=logs-eu --location=${var.region} --view=_AllLogs --limit=20`
      - Von Hand starten: `gcloud run jobs execute aufraeumen --region=${var.region}`
    EOT
  }

  notification_channels = [google_monitoring_notification_channel.email.id]
}
