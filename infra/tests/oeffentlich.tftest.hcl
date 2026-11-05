# KO3: Ein vom Kill-Switch gesperrter Dienst bleibt gesperrt, auch bei `tofu
# apply` (module/oeffentlich). Simuliert: Die Richtlinie in GCP kommt aus
# override_data, so wie sie der Kill-Switch hinterlässt.

mock_provider "google" {}

variables {
  name     = "server"
  location = "europe-west1"
  oeffnen  = false
}

run "laesst_offenen_dienst_offen" {
  command = plan
  module {
    source = "./module/oeffentlich"
  }

  override_data {
    target = data.google_cloud_run_v2_service_iam_policy.dienst
    values = {
      policy_data = "{\"bindings\":[{\"role\":\"roles/run.invoker\",\"members\":[\"allUsers\"]}]}"
    }
  }

  assert {
    condition     = google_cloud_run_v2_service_iam_member.oeffentlich.member == "allUsers" && google_cloud_run_v2_service_iam_member.oeffentlich.role == "roles/run.invoker"
    error_message = "Der Dienst ist nicht öffentlich aufrufbar."
  }
}

# KRITISCH: Der Kill-Switch hat allUsers entfernt, übrig ist nur das Konto des
# Deploys. apply darf den Dienst nicht wieder öffnen.
run "oeffnet_gesperrten_dienst_nicht" {
  command = plan
  module {
    source = "./module/oeffentlich"
  }

  override_data {
    target = data.google_cloud_run_v2_service_iam_policy.dienst
    values = {
      policy_data = "{\"bindings\":[{\"role\":\"roles/run.developer\",\"members\":[\"serviceAccount:deploy@test-projekt.iam.gserviceaccount.com\"]}]}"
    }
  }

  expect_failures = [google_cloud_run_v2_service_iam_member.oeffentlich]
}

run "oeffnet_dienst_ohne_jede_bindung_nicht" {
  command = plan
  module {
    source = "./module/oeffentlich"
  }

  override_data {
    target = data.google_cloud_run_v2_service_iam_policy.dienst
    values = {
      policy_data = "{\"etag\":\"BwX\"}"
    }
  }

  expect_failures = [google_cloud_run_v2_service_iam_member.oeffentlich]
}

run "aehnlicher_name_zaehlt_nicht_als_offen" {
  command = plan
  module {
    source = "./module/oeffentlich"
  }

  override_data {
    target = data.google_cloud_run_v2_service_iam_policy.dienst
    values = {
      policy_data = "{\"bindings\":[{\"role\":\"roles/run.invoker\",\"members\":[\"user:allUsers@example.com\"]}]}"
    }
  }

  expect_failures = [google_cloud_run_v2_service_iam_member.oeffentlich]
}

run "oeffnet_gesperrten_dienst_nur_auf_wunsch" {
  command = plan
  module {
    source = "./module/oeffentlich"
  }

  variables {
    oeffnen = true
  }

  override_data {
    target = data.google_cloud_run_v2_service_iam_policy.dienst
    values = {
      policy_data = "{\"etag\":\"BwX\"}"
    }
  }

  assert {
    condition     = google_cloud_run_v2_service_iam_member.oeffentlich.member == "allUsers"
    error_message = "Auf Wunsch wird der Dienst nicht wieder geöffnet."
  }
}
