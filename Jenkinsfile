#!groovy
@Library('github.com/cloudogu/ces-build-lib@2.3.0')
import com.cloudogu.ces.cesbuildlib.*


goVersion = "1.26.0"

node('docker') {

    repositoryOwner = 'cloudogu'
    repositoryName = 'pat-proxy'
    project = "github.com/${repositoryOwner}/${repositoryName}"
    githubCredentialsId = 'sonarqube-gh'
    projectName = 'patproxy'
    branch = "${env.BRANCH_NAME}"

    stage('Checkout') {
        checkout scm
    }

    Git git = new Git(this, 'cesmarvin')
    git.committerName = 'cesmarvin'
    git.committerEmail = 'cesmarvin@cloudogu.com'
    GitFlow gitflow = new GitFlow(this, git)
    GitHub github = new GitHub(this, git)
    Changelog changelog = new Changelog(this)

    Docker docker = new Docker(this)
    docker.image("golang:${goVersion}").mountJenkinsUser(true).inside("--volume ${WORKSPACE}:/go/src/${project}") {

        stage('Build') {
            withAuthenticatedGithub {
                make 'clean package'
                archiveArtifacts 'target/*.tar.gz'
            }
        }

        stage('Unit Test') {
            make 'unit-test'
            junit allowEmptyResults: true, testResults: 'target/*-tests.xml'
        }

        stage('Static Analysis') {
            withAuthenticatedGithub {
                def commitSha = sh(returnStdout: true, script: 'git rev-parse HEAD').trim()
                withCredentials([[$class: 'UsernamePasswordMultiBinding', credentialsId: githubCredentialsId, usernameVariable: 'USERNAME', passwordVariable: 'REVIEWDOG_GITHUB_API_TOKEN']]) {
                    withEnv(["CI_PULL_REQUEST=${env.CHANGE_ID}", "CI_COMMIT=${commitSha}", "CI_REPO_OWNER=${repositoryOwner}", "CI_REPO_NAME=${repositoryName}"]) {
                        make 'static-analysis'
                    }
                }
            }
        }
    }

    stage('SonarQube') {
        def scannerHome = tool name: 'sonar-scanner', type: 'hudson.plugins.sonar.SonarRunnerInstallation'
        withSonarQubeEnv {
            git.fetch()


            if (branch == "main") {
                echo "This branch has been detected as the main branch."
                sh "${scannerHome}/bin/sonar-scanner -Dsonar.projectKey=${projectName} -Dsonar.projectName=${projectName}"
            } else if (branch == "develop") {
                echo "This branch has been detected as the develop branch."
                sh "${scannerHome}/bin/sonar-scanner -Dsonar.projectKey=${projectName} -Dsonar.projectName=${projectName} -Dsonar.branch.name=${env.BRANCH_NAME} -Dsonar.branch.target=main  "
            } else if (env.CHANGE_TARGET) {
                echo "This branch has been detected as a pull request."
                sh "${scannerHome}/bin/sonar-scanner -Dsonar.projectKey=${projectName} -Dsonar.projectName=${projectName} -Dsonar.branch.name=${env.CHANGE_BRANCH}-PR${env.CHANGE_ID} -Dsonar.branch.target=${env.CHANGE_TARGET} "
            } else if (branch.startsWith("feature/")) {
                echo "This branch has been detected as a feature branch."
                sh "${scannerHome}/bin/sonar-scanner -Dsonar.projectKey=${projectName} -Dsonar.projectName=${projectName} -Dsonar.branch.name=${env.BRANCH_NAME} -Dsonar.branch.target=develop"
            } else {
                echo "This branch has been detected as a miscellaneous branch."
                sh "${scannerHome}/bin/sonar-scanner -Dsonar.projectKey=${projectName} -Dsonar.projectName=${projectName} -Dsonar.branch.name=${env.BRANCH_NAME} -Dsonar.branch.target=develop"
            }
        }
        timeout(time: 2, unit: 'MINUTES') { // Needed when there is no webhook for example
            def qGate = waitForQualityGate()
            if (qGate.status != 'OK') {
                unstable("Pipeline unstable due to SonarQube quality gate failure")
            }
        }
    }

    if (!env.CHANGE_ID && gitflow.isReleaseBranch()) {
        String releaseVersion = git.getSimpleBranchName()
        String releaseArtifact = "pat-proxy-${releaseVersion.substring(1)}.tar.gz"

        stage('Finish Release') {
            if (currentBuild.currentResult != 'SUCCESS') {
                error('Release requires a successful build and SonarQube quality gate.')
            }
            String versionLine = readFile('Makefile').readLines().find { it.startsWith('VERSION=') }
            if (!versionLine || releaseVersion != "v${versionLine.substring('VERSION='.length()).trim()}") {
                error('Release branch must match the VERSION in Makefile (release/v<VERSION>).')
            }
            changelog.changesForVersion(releaseVersion)
            gitflow.finishRelease(releaseVersion, 'main')
        }

        docker.image("golang:${goVersion}").mountJenkinsUser(true).inside("--volume ${WORKSPACE}:/go/src/${project}") {
            stage('Build after Release') {
                git.checkout(releaseVersion)
                withAuthenticatedGithub {
                    make 'clean package'
                }
                archiveArtifacts 'target/*.tar.gz'
            }
        }

        stage('Add Github-Release') {
            String releaseId = github.createReleaseWithChangelog(releaseVersion, changelog, 'main')
            if (!releaseId) {
                error('GitHub release creation failed.')
            }
            github.addReleaseAsset(releaseId, "target/${releaseArtifact}")
        }
    }
}

String repositoryOwner
String repositoryName
String project
String githubCredentialsId

void make(goal) {
    sh "cd /go/src/${project} && make ${goal}"
}

void withAuthenticatedGithub(Closure closure) {
    withCredentials([usernamePassword(credentialsId: 'cesmarvin', usernameVariable: 'GIT_AUTH_USR', passwordVariable: 'GITHUB_API_TOKEN')]) {
       try {
           createCredentialsFile("${GIT_AUTH_USR}", "${GITHUB_API_TOKEN}")
           closure.call()
       } catch (err) {
           throw err
       } finally {
           sh "rm -f .netrc"
       }
    }
}


void createCredentialsFile(String userName, String token) {
    writeFile encoding: 'UTF-8', file: '.netrc', text: """
machine github.com
login ${userName}
password ${token}
    """.trim()
}
