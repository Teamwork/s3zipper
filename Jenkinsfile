pipeline {
  // triggers { pollSCM('H/3 * * * *') }

  agent {
    kubernetes {
      label 'projects-s3zipper-worker'
      defaultContainer 'gitops'
      yamlFile '.jenkins/KubernetesPod.yaml'
    }
  }
  
  stages {

    stage('Build docker image') {
      when {
        not {
          changeRequest()
        }
        anyOf {
          branch 'feature/k8s-deployment'
          branch 'master'
        }
      }

      steps {
        container('gitops') {
          withCredentials([usernamePassword( credentialsId: 'jenkins-projects-dockerhub-creds', usernameVariable: 'DOCKER_USR', passwordVariable: 'DOCKER_PSW')]) {
              sh 'docker login --username ${DOCKER_USR} --password ${DOCKER_PSW}'
              //sh 'docker build -t teamwork/project-manager:$(git rev-parse HEAD) ./'
              sh 'make build push'
          }

          withCredentials([usernamePassword( credentialsId: 'jenkins-projects-github-creds', usernameVariable: 'GITHUB_USR', passwordVariable: 'GITHUB_PSW')]) {
            sh 'make git-prep git-push GH_TOKEN=$GITHUB_PSW BRANCH=$BRANCH_NAME'
          }
        }
      }
    }
  }
}
