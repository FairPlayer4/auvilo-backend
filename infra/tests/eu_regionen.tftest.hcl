# Anforderung D1: Alles bleibt in der EU.
#
# Ohne GCP-Organisation gibt es keine Standortrichtlinie. Dieser Test ersetzt
# sie: Er plant die Konfiguration mit simulierten Providern (kein GCP-Zugang
# nötig, läuft in der CI) und prüft die Standorte aller Ressourcen.
# Ausführen: im Verzeichnis infra `tofu init -backend=false` und `tofu test`.

# Simulierte Ressourcen bekommen sonst zufällige IDs; die folgenden Werte
# haben das Format, das der Provider bei Verweisen prüft.
mock_provider "google" {
  mock_data "google_project" {
    defaults = {
      number = "123456789"
    }
  }
  mock_resource "google_service_account" {
    defaults = {
      email  = "testkonto@test-projekt.iam.gserviceaccount.com"
      member = "serviceAccount:testkonto@test-projekt.iam.gserviceaccount.com"
      name   = "projects/test-projekt/serviceAccounts/testkonto@test-projekt.iam.gserviceaccount.com"
      id     = "projects/test-projekt/serviceAccounts/testkonto@test-projekt.iam.gserviceaccount.com"
    }
  }
  mock_resource "google_project_iam_custom_role" {
    defaults = {
      name = "projects/test-projekt/roles/killswitchCloudRun"
      id   = "projects/test-projekt/roles/killswitchCloudRun"
    }
  }
  mock_resource "google_iam_workload_identity_pool" {
    defaults = {
      name = "projects/123456789/locations/global/workloadIdentityPools/github"
    }
  }
  mock_resource "google_pubsub_topic" {
    defaults = {
      id = "projects/test-projekt/topics/budget-meldungen"
    }
  }
  mock_resource "google_artifact_registry_repository" {
    defaults = {
      id   = "projects/test-projekt/locations/europe-west1/repositories/container"
      name = "container"
    }
  }
}

mock_provider "archive" {
  mock_data "archive_file" {
    defaults = {
      output_md5 = "00000000000000000000000000000000"
    }
  }
}

variables {
  projekt        = "test-projekt"
  rechnungskonto = "000000-000000-000000"
  alarm_email    = "test@example.com"
}

run "alle_standorte_liegen_in_der_eu" {
  command = plan

  assert {
    condition     = google_firestore_database.standard.location_id == var.region
    error_message = "Firestore liegt nicht in var.region."
  }
  assert {
    condition     = google_artifact_registry_repository.container.location == var.region
    error_message = "Artifact Registry liegt nicht in var.region."
  }
  assert {
    condition     = google_logging_project_bucket_config.eu.location == var.region
    error_message = "Der Log-Bucket liegt nicht in var.region."
  }
  assert {
    condition     = upper(google_storage_bucket.quellen.location) == upper(var.region)
    error_message = "Der Quellen-Bucket liegt nicht in var.region."
  }
  assert {
    condition     = google_cloudfunctions2_function.killswitch.location == var.region
    error_message = "Die Kill-Switch-Funktion liegt nicht in var.region."
  }
  assert {
    condition     = google_cloudfunctions2_function.killswitch.event_trigger[0].trigger_region == var.region
    error_message = "Der Trigger der Funktion liegt nicht in var.region."
  }
  assert {
    condition     = google_pubsub_topic.budget.message_storage_policy[0].allowed_persistence_regions == toset([var.region])
    error_message = "Das Budget-Thema darf Nachrichten außerhalb von var.region speichern."
  }
}

run "lehnt_region_ausserhalb_der_eu_ab" {
  command = plan

  variables {
    region = "us-central1"
  }

  expect_failures = [var.region]
}

run "lehnt_europaeische_nicht_eu_region_ab" {
  command = plan

  variables {
    region = "europe-west2" # London: Europa, aber nicht EU
  }

  expect_failures = [var.region]
}

run "lehnt_budget_mit_nachkommastellen_ab" {
  command = plan

  variables {
    budget_eur = 20.5
  }

  expect_failures = [var.budget_eur]
}

run "lehnt_sperren_ueber_trennen_ab" {
  command = plan

  variables {
    schwelle_sperren = 2
    schwelle_trennen = 1.5
  }

  expect_failures = [var.schwelle_trennen]
}
