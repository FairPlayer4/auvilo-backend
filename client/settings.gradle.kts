// Kotlin-Teil des Backends (Etappe B2, docs/b2-entwurf.md):
//  - protokoll:  Schlüssel, Verpacken/Entpacken, Signaturen. Reines Kotlin ohne
//                Android und ohne Netz – soll später unverändert in die App.
//  - testclient: Kommandozeile, die ein Gerät gegenüber dem Server spielt.
//
// Bauen und testen (im Ordner client/): ./gradlew check
rootProject.name = "avlreminder-client"

pluginManagement {
    repositories {
        gradlePluginPortal()
        mavenCentral()
    }
}

dependencyResolutionManagement {
    repositoriesMode = RepositoriesMode.FAIL_ON_PROJECT_REPOS
    repositories {
        mavenCentral()
        // Nur für androidx.annotation (Abhängigkeit von tink-android).
        google {
            content { includeGroupByRegex("androidx\\..*") }
        }
    }
}

include("protokoll", "testclient")
