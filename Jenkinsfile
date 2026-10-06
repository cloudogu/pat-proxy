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
            Git git = new Git(this, "cesmarvin")
            git.fetch()


            if (branch == "master") {
                echo "This branch has been detected as the master branch."
                sh "${scannerHome}/bin/sonar-scanner -Dsonar.projectKey=${projectName} -Dsonar.projectName=${projectName}"
            } else if (branch == "develop") {
                echo "This branch has been detected as the develop branch."
                sh "${scannerHome}/bin/sonar-scanner -Dsonar.projectKey=${projectName} -Dsonar.projectName=${projectName} -Dsonar.branch.name=${env.BRANCH_NAME} -Dsonar.branch.target=master  "
            } else if (env.CHANGE_TARGET) {
                echo "This branch has been detected as a pull request."
                sh "${scannerHome}/bin/sonar-scanner -Dsonar.projectKey=${projectName} -Dsonar.projectName=${projectName} -Dsonar.branch.name=${env.CHANGE_BRANCH}-PR${env.CHANGE_ID} -Dsonar.branch.target=${env.CHANGE_TARGET} "
            } else if (branch.startsWith("feature/")) {
                echo "This branch has been detected as a feature branch."
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