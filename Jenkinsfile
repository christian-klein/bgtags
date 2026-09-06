pipeline {
    agent any

    environment {
        // App-specific identifier only.
        // REGISTRY_HOST and REGISTRY_NAMESPACE are dynamically injected
        // by Jenkins Global Environment Variables (e.g. forgejo.cklein.us / cdk2128).
        APP_NAME = 'bgtags'
    }

    stages {
        // ==========================================
        // BUILD BRANCH: Compile, Test, Package, Push
        // ==========================================
        stage('Test & Build Container') {
            when { branch 'build' }
            steps {
                script {
                    def gitCommit = sh(script: 'git rev-parse --short HEAD', returnStdout: true).trim()
                    def fullImage = "${env.REGISTRY_HOST}/${env.REGISTRY_NAMESPACE}/${env.APP_NAME}"

                    echo "Building ${fullImage}:${gitCommit} and ${fullImage}:latest..."
                    sh "docker build -t ${fullImage}:${gitCommit} -t ${fullImage}:latest ."

                    echo "Pushing image to registry..."
                    withCredentials([usernamePassword(
                        credentialsId: 'forgejo-registry-creds',
                        usernameVariable: 'REG_USER',
                        passwordVariable: 'REG_PASS'
                    )]) {
                        sh "echo \"${REG_PASS}\" | docker login ${env.REGISTRY_HOST} -u \"${REG_USER}\" --password-stdin"
                        sh "docker push ${fullImage}:${gitCommit}"
                        sh "docker push ${fullImage}:latest"
                    }
                }
            }
        }

        // ==========================================
        // DEPLOY BRANCH: Trigger Deployment via home_automations
        // ==========================================
        stage('Deploy Application') {
            when { branch 'deploy' }
            steps {
                echo "Triggering centralized deployment pipeline for ${env.APP_NAME}..."
                build job: 'deploy-service', parameters: [
                    string(name: 'SERVICE_NAME', value: env.APP_NAME),
                    string(name: 'IMAGE_TAG', value: 'latest')
                ]
            }
        }
    }
}
