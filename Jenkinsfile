node {
    checkout scm
        
    stage('Docker Build') {
        docker.build('dukfaar/apigateway')
    }

    stage('Update Service') {
        sh 'docker service update --force apigateway_apigateway'
    }
}
