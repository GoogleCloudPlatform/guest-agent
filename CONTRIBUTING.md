# How to become a contributor and submit your own code

## Before you begin

### Sign our Contributor License Agreement

Contributions to this project must be accompanied by a
[Contributor License Agreement](https://cla.developers.google.com/about) (CLA).
You (or your employer) retain the copyright to your contribution; this simply
gives us permission to use and redistribute your contributions as part of the
project.

If you or your current employer have already signed the Google CLA (even if it
was for a different project), you probably don't need to do it again.

Visit <https://cla.developers.google.com/> to see your current agreements or to
sign a new one.

### Review our community guidelines

This project follows
[Google's Open Source Community Guidelines](https://opensource.google/conduct/).

## Contributing a patch

1. Submit an issue describing your proposed change to the repo in question.
1. The repo owner will respond to your issue promptly.
1. If your proposed change is accepted, and you haven't already done so, sign a
   Contributor License Agreement (see details above).
1. Fork the desired repo, develop and test your code changes.
1. Ensure that your code adheres to the existing style in the sample to which
   you are contributing. Refer to the
   [Google Cloud Platform Samples Style Guide]
   (https://github.com/GoogleCloudPlatform/Template/wiki/style.html) for the
   recommended coding standards for this organization.
1. Ensure that your code has an appropriate set of unit tests which all pass.
1. Submit a pull request.

## Contributing a new sample App

1. Submit an issue to the `GoogleCloudPlatform/Template` repo describing your
   proposed sample app.
1. The Template repo owner will respond to your enhancement issue promptly.
   Instructional value is the top priority when evaluating new app proposals for
   this collection of repos.
1. If your proposal is accepted, and you haven't already done so, sign a
   Contributor License Agreement (see details above).
1. Create your own repo for your app following this naming convention:
    * {product}-{app-name}-{language}
    * products: appengine, compute, storage, bigquery, prediction, cloudsql
    * example:  appengine-guestbook-python
    * For multi-product apps, concatenate the primary products, like this:
      compute-appengine-demo-suite-python.
    * For multi-language apps, concatenate the primary languages like this:
      appengine-sockets-python-java-go.

1. Clone the `README.md`, `CONTRIB.md` and `LICENSE` files from the
   GoogleCloudPlatform/Template repo.
1. Ensure that your code adheres to the existing style in the sample to which
   you are contributing. Refer to the
   [Google Cloud Platform Samples Style Guide]
   (https://github.com/GoogleCloudPlatform/Template/wiki/style.html) for the
   recommended coding standards for this organization.
1. Ensure that your code has an appropriate set of unit tests which all pass.
1. Submit a request to fork your repo in GoogleCloudPlatform organization via
   your proposal issue.
