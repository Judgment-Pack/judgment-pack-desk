export interface ArtifactReference {sha256:string;bytes:number}
export interface ArtifactImpact {classification:'initial'|'patch'|'minor'|'major'|'unknown';rationale:string}
export interface ArtifactReview<T> {
 reference:ArtifactReference
 review:{version:1;target:string;basePublication:string;storageRevision:string;candidate:ArtifactReference;proposedVersion:string;impact:ArtifactImpact;dependencies:ArtifactReference[];preview:T}
}
/** Review evidence survives reload. It never carries a reusable write grant. */
export function reviewMatches<T>(review:ArtifactReview<T>,target:string,revision:string):boolean {
 return review.review.target===target&&review.review.storageRevision===revision
}
