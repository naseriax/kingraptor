# Kingraptor
Monitor Nokia 1830PSS equipment's RAM/CPU/Disk utilization in Go!

# Features
```
 - Controlled worker quantity.
 - Usable in Crontab or just single execution.
 - SSH Tunnel feature to access the Nodes via ssh gateway (NFM-T for example).
 - Ability to specify CPU/RAM/Disk Usage threshold per NE in the nodes.csv file.
 - Sends email if the utilizations pass the threshold.
 - Mail interval timer.
 - Mail buffer.
```

## Usage

# Compile for linux amd64 arch:
```
$ env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build .
```
# config.json:
```
{
    "verbose":true,                   //set to true to see more information in stdout during the execution.
    "logging" : false,                //set to true to have a timestamped csv log file per execution which includes the high utilization NE data.
    "enableMail": true,               //set to true to receive email in case of high utilization condition.
    "mailRelayIp": "10.10.10.10:25",  //IP:Port information of the SMTP relay server to receive and possibly redistribute the email messages.
    "ramCpuTimePeriod" : 1800,         //Integer. Execution duration(1) in seconds.
    "mailInterval" : 300,              //Integer. email sending interval(2) in seconds.
    "mailFrom": "from@test.com",      //The email messages with be sent from this email address.
    "mailTo": "to@test.com",          //The email messages with be sent to this email address.
    "logSize": 10,                    //Log file rotation will be executed if the log file size exceeds this value (MB)(3).
    "queryInterval": 300,              //Data sampling will be done on all the Nodes mentioned in nodes.csv file with this interval in seconds(1). 
    "inputFileName": "nodes.csv",     //The csv file name which contains the Nodes information. Check the file to know correct formatting.
    "workerQuantity": 10000,          //Determines how many Nodes will be queried and sampled in parallel(4).
    "sshTunnel" : true,               //set to false to connect to the nodes directly.
	"sshGwIp":"11.11.11.11",          //IP address of the ssh gateway machine.
	"sshGwUser":"root",               //Ssh gateway machine username.
	"sshGwPass" :"*********",         //Ssh gateway machine Password.
	"sshGwPort":"22"                  //string. Ssh gateway machine Port number.
}

(1): The collection will be done is cycles which is calculated as below:
        ramCpuTimePeriod / queryInterval
     For example is ramCpuTimePeriod=1800 (30 minutes) and queryInterval=300(5 minutes), total of 7 cycles will be executed (6 + 1 extra for cooldown).
     If in all of the cycles, the RAM/CPU utilization of any node were above the specified threshold, 1 email will be sent per resource when all cycles are finished.
(2): Disk utilization emails will be sent in case high disk usage were observed immediately. 
     To avoid receiving many repeated emails after each cycle, the script will wait for this timer to finish before sending a new email about Disk usage.
(3): Experimental. refactoring is planned.
(4): In case of ssh tunneling, it's restricted to 5.
```

# File composition:
Make sure the executable, config.json and nodes.csv files are all located in the same path.

# Execution in crontab:
```
#To execute the program every 45 minutes.
$ crontab -l
*/45 * * * * /home/user/Go-Projects/kingraptor/kingraptor > /home/user/output.log 2>&1
```