import java.util.Properties

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// 版本号与仓库根 VERSION 单一真源同步（文件带 BOM，显式去除）。
val versionText: String = File(projectDir, "../../VERSION").readText().replace("\uFEFF", "").trim()
val versionParts = versionText.split(".").map { it.toInt() }

android {
    namespace = "com.lunitide.app"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.lunitide.app"
        minSdk = 26
        targetSdk = 36
        versionCode = versionParts[0] * 10000 + versionParts[1] * 100 + versionParts[2]
        versionName = versionText
    }

    val keystoreFile = File(rootDir, "../.release-cache/lunitide-release.jks")
    val keystoreProps = Properties().apply {
        val f = File(rootDir, "../.release-cache/keystore.properties")
        if (f.exists()) f.inputStream().use { load(it) }
    }
    val releaseSigning = signingConfigs.create("release") {
        storeFile = keystoreFile
        storePassword = keystoreProps.getProperty("storePassword", "")
        keyAlias = keystoreProps.getProperty("keyAlias", "lunitide")
        keyPassword = keystoreProps.getProperty("keyPassword", "")
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = releaseSigning
        }
        debug {
            signingConfig = releaseSigning
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    // 唯一的三方依赖：扫码配对（内置相机扫码 Activity + ZXing 解码）。
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
}
