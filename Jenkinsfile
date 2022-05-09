/* groovylint-disable CatchException */
/* groovylint-disable-next-line CompileStatic */
pipeline {
    agent any
    tools { nodejs 'node' }
    environment {

        // DOT_ENV = credentials('API_CKLEIN_US_ENV')

        REGISTRY_CREDENTIALS = 'registry.cklein.us'
        /* groovylint-disable-next-line DuplicateStringLiteral */
        REGISTRY_DNS = 'registry.cklein.us'
        IMAGE_NAME = 'bgtags.cklein.us'
        IMAGE_PATH = "cklein.us/$IMAGE_NAME"
        REGISTRY_URL = "https://$REGISTRY_DNS/"
        DOCKER_SERVER = 'docker.cklein.us'

        NOTICE_START = '********************************************************************\n*            '
        NOTICE_END = '            *\n********************************************************************'
        NOTICE_SEP = '***************************************'

        GIT_BRANCH_DEV = 'dev'
        GIT_BRANCH_MASTER = 'main'

        JWT_SECRET = credentials('JWT_SECRET')
    }
    stages {
        stage('Environment') {
            steps {
                echo "$NOTICE_START Check Environment $NOTICE_END"
                sh 'mkdir -p ~/.ssh'
                sh 'touch ~/.ssh/known_hosts'
                echo "$NOTICE_SEP"
                sh "ssh-keygen -R $DOCKER_SERVER"
                echo "$NOTICE_SEP"
                sh "ssh-keyscan -t rsa $DOCKER_SERVER >> ~/.ssh/known_hosts"
                echo "$NOTICE_SEP"
                sh 'git --version'
                echo "$NOTICE_SEP"
                sh 'docker -v'
                echo "$NOTICE_SEP"
                sh 'printenv'
            }
        }
        stage('Build React app') {
            when {
                expression { env.BRANCH_NAME == "$GIT_BRANCH_MASTER" }
            }
            steps {
                echo "$NOTICE_START Build React App $NOTICE_END"

                sh 'npm install'
                sh 'npm run build'
            }
        }
        stage('Build New Image') {
            when {
                expression { env.BRANCH_NAME == "$GIT_BRANCH_MASTER" }
            }
            steps {
                echo "$NOTICE_START Build New Image $NOTICE_END"
                script {
                    app = docker.build "$IMAGE_PATH"
                }
            }
        }
        stage('Test') {
            when {
                expression { env.BRANCH_NAME == "$GIT_BRANCH_DEV" }
            }
            steps {
                echo 'Testing..'
            }
        }
        stage('Deploy to Registry') {
            when {
                expression { env.BRANCH_NAME == "$GIT_BRANCH_MASTER" }
            }
            steps {
                echo "$NOTICE_START Deploy to Registry $NOTICE_END"
                script {
                    docker.withRegistry("$REGISTRY_URL", REGISTRY_CREDENTIALS) {
                        echo "$NOTICE_SEP"
                        app.push("${env.BUILD_NUMBER}")
                        echo "$NOTICE_SEP"
                        app.push('latest')
                    }
                }
            }
        }
                stage('Remove Unused docker image') {
            steps {
                sh 'docker images'
                echo "$NOTICE_START Remove Unused docker image $NOTICE_END"
                script {
                    try {
                        sh "docker rmi ${app.imageName()}"
                    } catch (Exception e) {
                        "No image ${app.imageName()} found"
                    }
                    docker.withRegistry("$REGISTRY_URL", REGISTRY_CREDENTIALS) {
                        echo "$NOTICE_SEP"
                        try {
                            sh "docker rmi ${app.imageName()}:${env.BUILD_NUMBER}"
                            echo "$NOTICE_SEP"
                        } catch (Exception e) {
                            "No image ${app.imageName()}:${env.BUILD_NUMBER} found"
                        }
                        try {
                            sh "docker rmi ${app.imageName()}:latest"
                        } catch (Exception e) {
                            "No image ${app.imageName()}:latest found"
                        }
                    }
                }
                script {
                    echo "$NOTICE_SEP"
                    sh 'docker ps --all'
                    try {
                        echo "$NOTICE_SEP"
                        sh 'docker images |' +
                        'grep "api.cklein.us"|' +
                        'awk \'!id[$3]++ {print $3}\'|' +
                        'xargs -t -I {} docker container ls --all --filter ancestor={} --format \'{{.ID}}\'|' +
                        'xargs docker stop|xargs docker rm'
                    } catch (Exception e) {
                        echo '!!!!! Exception occurred stopping and removing existing containers'
                        echo '!!!!! ' + e                    }
                    echo "$NOTICE_SEP"
                    sh 'docker image prune -f'
                    try {
                        echo "$NOTICE_SEP"
                        sh 'docker rmi $(docker images -a | grep "^<none>" | awk \'{print $3}\')'
                    } catch (Exception e) {
                        echo '!!!!! Exception occurred removing <none> images: '
                        /* groovylint-disable-next-line DuplicateStringLiteral */
                        echo '!!!!! ' + e
                    }
                    try {
                        echo "$NOTICE_SEP"
                        sh "docker rmi -f \$(docker images -a | grep \"${app.imageName()}\" | awk '{print \$3}')"
                    } catch (Exception e) {
                        echo '!!!!! Exception occurred removing app.imageName: '
                        /* groovylint-disable-next-line DuplicateStringLiteral */
                        echo '!!!!! ' + e
                    }
                }
                echo "$NOTICE_SEP"
                /* groovylint-disable-next-line DuplicateStringLiteral */
                sh 'docker images'
            }
                }
        stage('Deploy new container using Ansible') {
            environment {
                ENV_FILE_ID = credentials('API_CKLEIN_US_ENV')
            }
            steps {
                echo ".env property file: ${ENV_FILE_ID}"
                echo '#####Copying .env property file to src\\main\\resources#####'
                sh "mv ${ENV_FILE_ID} .env"
                echo "$NOTICE_START Deploy new container using Ansible $NOTICE_END"
                ansiblePlaybook(
                    playbook: 'prd/playbook/api.cklein.us.yml',
                    inventory: 'prd/playbook/inventory',
                    credentialsId: 'ansible',
                    hostKeyChecking: 'false',
                    colorized: true
                )
            }
        }
    }
// post {
//     always {
//     }
//     success {
//     }
//     failure {
//     }
// }
}
